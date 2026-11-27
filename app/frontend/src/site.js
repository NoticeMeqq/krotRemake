import {ChooseSiteDirectory, DeploySiteDirectory, ReadSite, SaveSiteHTML, SiteDomains} from '../wailsjs/go/main/App';
import {$, el, errText} from './util.js';

export async function openSite(node) {
    const view = el('div', {class: 'site-editor'}, el('h2', {}, 'Сайт'), el('p', {class: 'muted'}, 'Ищу сайты на узле…'));
    $('modal-body').replaceChildren(view);
    $('modal').hidden = false;
    let domains;
    try {
        domains = (await SiteDomains(node.name)) || [];
    } catch (error) {
        view.replaceChildren(el('h2', {}, 'Сайт'), el('p', {class: 'err'}, errText(error)));
        return;
    }
    if (!view.isConnected) return;
    if (!domains.length) {
        view.replaceChildren(el('h2', {}, 'Сайт'), el('p', {class: 'muted'}, 'На этом узле нет сайта, развёрнутого через nginx-tls.'));
        return;
    }

    const domain = el('select', {id: 'site-domain'}, domains.map((name) => el('option', {value: name}, name)));
    const source = el('textarea', {id: 'site-html', rows: '14', spellcheck: 'false'});
    const save = el('button', {}, 'Сохранить HTML');
    const choose = el('button', {class: 'ghost'}, 'Выбрать папку…');
    const folder = el('div', {});
    const indexError = el('p', {class: 'err site-index-error'});
    const message = el('p', {role: 'status'});
    let page = null;
    let request = 0;
    let busy = false;
    let editing = false;
    let path = '';

    function controls() {
        domain.disabled = busy;
        choose.disabled = busy || editing;
        source.disabled = busy || editing || !page;
        save.disabled = source.disabled || source.value === page?.html;
    }

    function report(text, error = false) {
        message.className = error ? 'err' : text ? 'checks ok' : '';
        message.textContent = text;
    }

    async function loadPage() {
        const id = ++request;
        editing = true;
        page = null;
        source.value = '';
        indexError.textContent = '';
        controls();
        try {
            const next = await ReadSite(node.name, domain.value);
            if (id !== request || !view.isConnected) return;
            page = next;
            source.value = page.html;
            // The textarea normalizes line endings.
            page.html = source.value;
        } catch (error) {
            if (id === request) indexError.textContent = errText(error);
        } finally {
            if (id === request) {
                editing = false;
                controls();
            }
        }
    }

    function showFolder() {
        if (!path) { folder.replaceChildren(); return; }
        const submit = el('button', {onclick: async () => {
            if (busy) return;
            busy = true;
            controls();
            submit.disabled = cancel.disabled = true;
            message.className = 'muted';
            message.textContent = 'Публикую файлы…';
            try {
                const count = await DeploySiteDirectory(node.name, node.host, domain.value, path);
                path = '';
                showFolder();
                report(`Сайт опубликован · файлов: ${count}`);
                await loadPage();
            } catch (error) {
                report(errText(error), true);
            } finally {
                busy = false;
                submit.disabled = cancel.disabled = false;
                controls();
            }
        }}, 'Опубликовать папку');
        const cancel = el('button', {class: 'ghost', onclick: () => { path = ''; showFolder(); }}, 'Отмена');
        folder.replaceChildren(el('div', {class: 'site-publish'},
            el('strong', {}, path),
            el('p', {class: 'muted'}, `Файлы в этой папке заменят содержимое сайта ${domain.value} и будут доступны посетителям.`),
            el('div', {class: 'row'}, submit, cancel)));
    }

    domain.addEventListener('change', () => {
        path = '';
        showFolder();
        report('');
        loadPage();
    });
    source.addEventListener('input', controls);
    save.addEventListener('click', async () => {
        if (!page || busy || editing) return;
        busy = true;
        controls();
        message.className = 'muted';
        message.textContent = 'Сохраняю HTML…';
        try {
            await SaveSiteHTML(node.name, node.host, domain.value, page.version, source.value);
            report('HTML сохранён');
            await loadPage();
        } catch (error) {
            report(errText(error), true);
        } finally {
            busy = false;
            controls();
        }
    });
    choose.addEventListener('click', async () => {
        busy = true;
        controls();
        try {
            const selected = await ChooseSiteDirectory();
            if (selected) {
                path = selected;
                showFolder();
            }
        } catch (error) {
            report(errText(error), true);
        } finally {
            busy = false;
            controls();
        }
    });
    view.replaceChildren(el('h2', {}, `Сайт · ${node.name}`),
        el('div', {class: 'site-domain'}, el('label', {for: 'site-domain'}, 'Домен'), domain),
        el('label', {for: 'site-html'}, 'index.html'),
        source, indexError,
        el('div', {class: 'row'}, save, choose),
        el('p', {class: 'muted'}, 'Сохранение HTML сохраняет остальные файлы. Для публикации сайта целиком выберите папку сборки с index.html.'),
        folder, message);
    await loadPage();
}
