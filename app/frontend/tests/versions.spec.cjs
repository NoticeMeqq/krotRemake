const {test, expect, entry} = require('./desktop-fixture.cjs');

const installed = '0.3.0';
const releaseURL = 'https://github.com/NoticeMeqq/krotRemake/releases/tag/v0.4.0';
const newerRelease = {current: installed, latest: '0.4.0', newer: true, canApply: true, url: releaseURL};

test('checks KrotRemake releases on demand and updates from the versions dialog', async ({page, openDesktop}) => {
    let release = {current: installed, latest: installed, newer: false};
    let finishUpdate;
    const calls = await openDesktop({
        Version: () => installed,
        Versions: () => ({nodes: [entry.name], rows: [
            {name: 'Xray', ref: '1.2.3', nodes: {[entry.name]: '1.2.2'}, latest: '1.2.4'},
        ]}),
        CheckUpdate: () => release,
        ApplyUpdate: () => new Promise((resolve, reject) => { finishUpdate = {resolve, reject}; }),
    });
    await page.getByRole('button', {name: 'Версии', exact: true}).click();
    const panel = page.getByRole('region', {name: 'KrotRemake'});
    const check = panel.getByRole('button', {name: 'Проверить обновления'});
    await expect(panel.getByText('v0.3.0', {exact: true})).toHaveCount(2);
    await expect(panel.getByText('Обновлений нет.')).toBeVisible();
    await expect(page.getByRole('table').getByRole('cell', {name: 'Xray'})).toBeVisible();
    const previousChecks = calls('CheckUpdate').length;

    release = newerRelease;
    await check.click();
    await expect(panel.getByText('v0.4.0', {exact: true})).toBeVisible();
    expect(calls('CheckUpdate')).toHaveLength(previousChecks + 1);
    await panel.getByRole('button', {name: 'Обновить', exact: true}).click();
    await expect(panel.getByRole('button', {name: 'Обновляю…'})).toBeDisabled();
    await expect(page.locator('#appupd').getByRole('button', {name: 'Обновляю…'})).toBeDisabled();
    await expect.poll(() => Boolean(finishUpdate)).toBe(true);
    await page.evaluate(() => window.emitKrotEvent('update-log', 'Сверяю SHA256SUMS'));
    await expect(panel.getByText('Сверяю SHA256SUMS')).toBeVisible();
    finishUpdate.reject(new Error('Тестовая ошибка загрузки'));
    await expect(panel.getByRole('alert')).toHaveText(/Тестовая ошибка загрузки/);
    await expect(panel.getByRole('button', {name: 'Обновить', exact: true})).toBeEnabled();

    finishUpdate = null;
    await panel.getByRole('button', {name: 'Обновить', exact: true}).click();
    await expect(panel.getByRole('alert')).toBeHidden();
    await expect.poll(() => Boolean(finishUpdate)).toBe(true);
    finishUpdate.resolve();
    expect(calls('ApplyUpdate')).toHaveLength(2);
});

test('shows the release download when in-place updating is unavailable without an inventory', async ({page, openDesktop}) => {
    const calls = await openDesktop({
        GetState: () => ({nodes: []}),
        Version: () => installed,
        Versions: () => { throw new Error('Не открыт файл узлов'); },
        CheckUpdate: () => ({...newerRelease, canApply: false, reason: 'Нет сборки для этой системы'}),
        BrowserOpenURL: () => {},
    });
    await page.getByRole('button', {name: 'Версии', exact: true}).click();
    const panel = page.getByRole('region', {name: 'KrotRemake'});
    await expect(page.locator('#modal').getByText('Не открыт файл узлов')).toBeVisible();
    await expect(panel.getByText('v0.4.0', {exact: true})).toBeVisible();
    await expect(panel.getByText('Нет сборки для этой системы')).toBeVisible();
    await panel.getByRole('button', {name: 'Скачать'}).click();
    await expect.poll(() => calls('BrowserOpenURL')).toEqual([[releaseURL]]);
    expect(calls('ApplyUpdate')).toEqual([]);
});

test('shows a failed release check and allows retrying it', async ({page, openDesktop}) => {
    let release = {current: installed, newer: false, reason: 'GitHub недоступен'};
    await openDesktop({
        Version: () => installed,
        Versions: () => ({nodes: [], rows: []}),
        CheckUpdate: () => release,
    });
    await page.getByRole('button', {name: 'Версии', exact: true}).click();
    const panel = page.getByRole('region', {name: 'KrotRemake'});
    await expect(panel.getByText('GitHub недоступен')).toBeVisible();
    await expect(panel.getByText('Обновлений нет.')).toHaveCount(0);
    release = {current: installed, latest: installed, newer: false};
    await panel.getByRole('button', {name: 'Проверить обновления'}).click();
    await expect(panel.getByText('Обновлений нет.')).toBeVisible();
    await expect(panel.getByText('GitHub недоступен')).toHaveCount(0);
});
