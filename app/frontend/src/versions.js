// Окно «Версии»: эталон (что ставит этот пульт), что стоит на узлах, что вышло у авторов.
import {Version, Versions} from '../wailsjs/go/main/App';
import {appUpdateControls, checkAppUpdate} from './appupdate.js';
import {$, el, errText, toast} from './util.js';

const versionText = (version) => !version ? '—' : (version === 'dev' ? version : 'v' + version);

function appVersionPanel() {
    const installed = el('strong', {}, '—');
    const latest = el('strong', {}, '—');
    const summary = el('p', {role: 'status', class: 'muted'});
    const error = el('p', {role: 'alert', class: 'err', hidden: ''});
    const actions = el('div', {class: 'row'});
    const check = el('button', {class: 'ghost small', onclick: refresh}, 'Проверить обновления');

    async function refresh() {
        check.disabled = true;
        actions.replaceChildren(check);
        latest.textContent = '—';
        summary.className = 'muted';
        summary.textContent = 'Проверяю релизы KrotRemake…';
        error.hidden = true;
        try {
            const result = await checkAppUpdate();
            if (result.current) installed.textContent = versionText(result.current);
            latest.textContent = versionText(result.latest);
            if (result.newer) {
                summary.textContent = result.canApply ? 'Доступно обновление KrotRemake.'
                    : (result.reason || 'Доступна новая версия на GitHub.');
                actions.append(...appUpdateControls(result, (message) => {
                    error.textContent = message;
                    error.hidden = !message;
                    if (message && !error.isConnected) toast(message, true);
                }));
            } else if (result.latest) {
                summary.textContent = 'Обновлений нет.';
            } else {
                summary.textContent = result.reason || 'Не удалось проверить обновления.';
            }
        } catch (e) {
            summary.className = 'err';
            summary.textContent = `Не удалось проверить обновления: ${errText(e)}`;
        } finally {
            check.disabled = false;
        }
    }

    Version().then((value) => { installed.textContent = versionText(value); }).catch(() => {});
    refresh();
    return el('section', {class: 'card', 'aria-labelledby': 'app-version-title'},
        el('h3', {id: 'app-version-title'}, 'KrotRemake'),
        el('dl', {class: 'facts'}, el('dt', {}, 'Установлено'), el('dd', {}, installed),
            el('dt', {}, 'Последний релиз'), el('dd', {}, latest)),
        summary, actions, error);
}

export async function openVersions() {
    const components = el('section', {}, el('div', {class: 'muted'}, 'Спрашиваю узлы и GitHub…'));
    $('modal-body').replaceChildren(el('h2', {}, 'Версии'), appVersionPanel(), components);
    $('modal').hidden = false;
    let v;
    try { v = await Versions(); } catch (e) {
        components.replaceChildren(el('h3', {}, 'Компоненты узлов'), el('div', {class: 'err'}, errText(e)));
        return;
    }
    const nodes = v.nodes || [];
    const head = el('tr', {}, el('th', {}, 'Компонент'), el('th', {}, 'Эталон'),
        nodes.map((n) => el('th', {}, n)), el('th', {}, 'Свежий'));
    const rows = (v.rows || []).map((r) => {
        const cell = (x) => el('td', {class: x && x !== r.ref ? 'checks bad' : ''}, x || '—');
        return [
            el('tr', {}, el('td', {}, r.name), el('td', {}, el('code', {}, r.ref)),
                nodes.map((n) => cell(r.nodes?.[n])),
                el('td', {class: r.latest && r.latest !== r.ref ? 'checks' : 'muted'}, r.latest || '?')),
            el('tr', {}, el('td', {colspan: String(3 + nodes.length), class: 'muted small'}, r.note)),
        ];
    });
    const errs = Object.entries(v.errors || {}).map(([n, e]) => el('div', {class: 'err small'}, `${n}: ${e}`));
    components.replaceChildren(
        el('h3', {}, 'Компоненты узлов'),
        el('p', {class: 'muted'}, 'Эталон — версии, которые ставит это приложение. Новую версию сначала проверяют на тестовых серверах, потом поднимают в эталоне — и она приходит на узлы обновлением.'),
        ...errs,
        el('table', {class: 'dns'}, head, rows.flat()));
}
