import {SaveSubscriptionSettings, SubscriptionSettings} from '../wailsjs/go/main/App';
import {$, closeModal, el, errText, toast} from './util.js';

export async function openSubscriptionSettings(node, name, supported) {
    const title = name ? 'Параметры подписки · ' + name : 'Мастер-профиль';
    const heading = el('h2', {}, title);
    const panel = el('div', {class: 'subscription-settings'}, heading,
        el('p', {class: 'muted'}, supported ? 'Загружаю параметры…' : 'Обновите модуль «Подписки» на этом узле.'));
    $('modal-body').replaceChildren(panel);
    $('modal').hidden = false;
    if (!supported) return;

    let current;
    try {
        current = await SubscriptionSettings(node, name);
    } catch (e) {
        if (panel.isConnected) panel.replaceChildren(heading, el('p', {class: 'err'}, errText(e)));
        return;
    }
    if (!panel.isConnected) return;

    const mtu = el('input', {type: 'number', min: '576', max: '1500', step: '1', placeholder: 'Из профиля', value: current.mtu});
    const dns = el('input', {type: 'text', maxlength: '32', spellcheck: 'false', placeholder: 'DNS узла', value: current.dns?.join(', ')});
    const keepalive = el('input', {type: 'number', min: '0', max: '65535', step: '1', placeholder: 'Из профиля', value: current.keepalive});
    const error = el('p', {class: 'err', role: 'alert', hidden: ''});
    const save = el('button', {type: 'submit'}, 'Сохранить');
    const form = el('form', {onsubmit: async (e) => {
        e.preventDefault();
        const settings = {};
        if (mtu.value !== '') settings.mtu = mtu.valueAsNumber;
        if (dns.value.trim()) settings.dns = dns.value.trim().split(/[,\s]+/);
        if (keepalive.value !== '') settings.keepalive = keepalive.valueAsNumber;
        error.hidden = true;
        save.disabled = true;
        try {
            await SaveSubscriptionSettings(node, name, settings);
            if (panel.isConnected) closeModal();
            toast(name ? 'Параметры сохранены. Обновите подписку в INCY.' : 'Мастер-профиль сохранён');
        } catch (e) {
            error.textContent = errText(e);
            error.hidden = false;
        } finally {
            save.disabled = false;
        }
    }},
    el('label', {}, 'MTU, байт', mtu),
    el('label', {}, 'DNS, один или два IPv4-адреса', dns),
    el('label', {}, 'Persistent keepalive, сек. (0 — выкл.)', keepalive),
    error,
    el('div', {class: 'row'}, save, el('button', {type: 'button', class: 'ghost', onclick: closeModal}, 'Отмена')));
    panel.replaceChildren(heading,
        el('p', {class: 'muted'}, name
            ? 'Параметры AmneziaWG в подписке. Пустые поля используют значения исходного профиля.'
            : 'Новые подписки на этом узле копируют эти параметры AmneziaWG при создании. Пустые поля используют значения исходного профиля.'),
        form);
    mtu.focus();
}
