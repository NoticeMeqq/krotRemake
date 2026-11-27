// Обновление приложения из шапки и окна «Версии».
import {Version, CheckUpdate, ApplyUpdate} from '../wailsjs/go/main/App';
import {BrowserOpenURL, EventsOn} from '../wailsjs/runtime/runtime';
import {$, el, toast, errText} from './util.js';

const EVERY = 6 * 60 * 60 * 1000;
let pendingCheck;
let applying = false;
let progress = '';

export function checkAppUpdate() {
    pendingCheck ||= CheckUpdate().then((result) => {
        renderUpdateBanner(result);
        return result;
    }).finally(() => { pendingCheck = null; });
    return pendingCheck;
}

export async function initAppUpdate() {
    try {
        const v = await Version();
        $('ver').textContent = v === 'dev' ? 'dev' : 'v' + v;
    } catch (e) { /* без версии — не беда */ }
    const check = () => checkAppUpdate().catch(() => {});
    check();
    setInterval(check, EVERY);
}

// проверка без мыши: KROT_APP_OPEN=update — сразу обновиться, если есть куда
export async function updateNow(onError) {
    if (applying) return;
    onError?.('');
    applying = true;
    progress = 'Начинаю обновление…';
    renderUpdateProgress();
    try {
        await ApplyUpdate(); // при успехе приложение перезапускается
    } catch (e) {
        applying = false;
        progress = '';
        renderUpdateProgress();
        const message = 'Обновление не удалось: ' + errText(e);
        if (onError) onError(message);
        else toast(message, true);
    }
}

function renderUpdateProgress() {
    for (const button of document.querySelectorAll('[data-app-update]')) {
        button.disabled = applying;
        button.textContent = applying ? 'Обновляю…' : 'Обновить';
    }
    for (const status of document.querySelectorAll('[data-app-update-progress]')) status.textContent = progress;
}

EventsOn('update-log', (line) => {
    if (applying) {
        progress = line;
        renderUpdateProgress();
    }
});

export function appUpdateControls(check, onError) {
    if (check.canApply) return [
        el('button', {class: 'small', 'data-app-update': '', disabled: applying ? '' : null, onclick: () => updateNow(onError)},
            applying ? 'Обновляю…' : 'Обновить'),
        el('span', {class: 'small', role: 'status', 'data-app-update-progress': ''}, progress),
    ];
    return check.url ? [el('button', {class: 'small', onclick: () => BrowserOpenURL(check.url)}, 'Скачать')] : [];
}

function renderUpdateBanner(result) {
    const bar = $('appupd');
    if (!result.newer) { bar.hidden = true; return; }
    bar.replaceChildren(
        el('span', {}, `Доступна новая версия Крота: ${result.latest} (у вас ${result.current}).`),
        ...(result.canApply ? [] : [el('span', {class: 'small'}, result.reason)]),
        ...appUpdateControls(result));
    bar.hidden = false;
}
