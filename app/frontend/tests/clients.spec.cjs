const {test, expect, entry} = require('./desktop-fixture.cjs');

test('creates and manages a subscription with its AmneziaVPN profile', async ({page, openDesktop}) => {
    let device = '';
    let published = false;
    const card = () => ({
        node: entry.name, kind: 'sub', name: device, body: 'https://vpn.example/u/fixture',
        amnezia_url: published ? 'https://vpn.example/u/fixture.vpnuri' : '',
        compat_url: published ? 'https://vpn.example/u/compat' : '',
    });
    const calls = await openDesktop({
        Clients: () => ({combined: true, delivery: true, sub: device ? [device] : [],
            awg: device ? [device] : [], published: published ? [device] : [], errors: {}}),
        AddClient: (_node, _kind, name) => { device = name; published = true; return card(); },
        PublishSubAmnezia: (_node, _name, publish) => { published = publish; return card(); },
    });
    const panel = page.locator('.access-card');
    const form = panel.locator('form').filter({visible: true});
    await expect(form.getByLabel('Тип подключения')).toHaveValue('combined');
    await form.getByLabel('Имя устройства').fill('_Phone');
    await form.getByRole('button', {name: 'Создать', exact: true}).click();
    await expect(page.getByText(/Первый символ не может быть подчёркиванием/)).toBeVisible();
    expect(calls('AddClient')).toEqual([]);
    await form.getByLabel('Имя устройства').fill('Phone');
    await form.getByRole('button', {name: 'Создать', exact: true}).click();

    const dialog = page.locator('#modal');
    await expect(dialog.getByText('✓ Клиент создан')).toBeVisible();
    await expect(dialog.getByRole('button', {name: 'Копировать ссылку AmneziaVPN'})).toBeVisible();
    await expect(dialog.getByRole('button', {name: 'Копировать подписку без AWG'})).toBeVisible();
    expect(calls('AddClient')).toEqual([[entry.name, 'combined', 'Phone']]);

    await dialog.getByRole('button', {name: 'Убрать AmneziaWG из подписки'}).click();
    const publish = dialog.getByRole('button', {name: 'Добавить AmneziaWG в подписку'});
    await expect(publish).toBeVisible();
    await publish.click();
    await expect(dialog.getByRole('button', {name: 'Копировать ссылку AmneziaVPN'})).toBeVisible();
    expect(calls('PublishSubAmnezia')).toEqual([[entry.name, 'Phone', false], [entry.name, 'Phone', true]]);
    await page.keyboard.press('Escape');
    const deviceRow = panel.locator('.access-row').filter({hasText: 'Phone'});
    await expect(deviceRow.getByText('Ссылка опубликована')).toBeVisible();
    await expect(deviceRow.getByRole('button', {name: 'Открыть профиль'})).toBeVisible();
});

test('keeps separate connection options on a node without combined subscriptions', async ({page, openDesktop}) => {
    await openDesktop({Clients: () => ({delivery: true, errors: {}})});
    const type = page.locator('.access-card form').filter({visible: true}).getByLabel('Тип подключения');
    await expect(type).toHaveValue('sub');
    await expect(type.locator('option')).toHaveText(['Подписка', 'Профиль AmneziaVPN']);
});

test('edits the master profile and an individual subscription independently', async ({page, openDesktop}) => {
    const calls = await openDesktop({
        Clients: () => ({delivery: true, combined: true, settings: true, sub: ['Kisik-Mobile'], errors: {}}),
        SubscriptionSettings: (_node, name) => name ? {mtu: 1260, keepalive: 0} : {},
        SaveSubscriptionSettings: (_node, _name, settings) => settings,
    });
    const dialog = page.locator('#modal');
    await page.getByRole('button', {name: 'Мастер-профиль'}).click();
    await expect(dialog.getByRole('heading', {name: 'Мастер-профиль'})).toBeVisible();
    await dialog.getByLabel('MTU, байт').fill('1250');
    await dialog.getByRole('button', {name: 'Сохранить'}).click();
    await expect(dialog).toBeHidden();
    await page.getByRole('button', {name: 'Параметры подписки Kisik-Mobile'}).click();
    await expect(dialog.getByLabel('MTU, байт')).toHaveValue('1260');
    await expect(dialog.getByLabel(/Persistent keepalive/)).toHaveValue('0');
    await dialog.getByLabel('MTU, байт').fill('1240');
    await dialog.getByLabel(/DNS,/).fill('1.1.1.1, 1.0.0.1');
    await dialog.getByRole('button', {name: 'Сохранить'}).click();
    await expect(dialog).toBeHidden();
    expect(calls('SaveSubscriptionSettings')).toEqual([
        [entry.name, '', {mtu: 1250}],
        [entry.name, 'Kisik-Mobile', {mtu: 1240, dns: ['1.1.1.1', '1.0.0.1'], keepalive: 0}],
    ]);
});
