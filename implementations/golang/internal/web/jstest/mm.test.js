/*
  mm.test.js — the behavioural suite for mm.js (T-0111, Option B).

  mm.js is the largest untested surface in the service: everything else has
  tests, the client script has source-shape greps. This suite drives the real
  script against a real DOM (jsdom) and a double for htmx, and asserts the
  effects §7.3 and the data-* attributes specify — never implementation
  internals.

  What it can and cannot catch (details/T-0111.md): wiring bugs — a handler
  that does nothing, a missing attribute, an operation posting an empty body
  — need no layout and this catches them. Layout-coupled bugs — comparing
  coordinates while mutating the DOM being measured (T-0110) — jsdom cannot
  see at all; those are prevented structurally. The drag tests here drive
  events explicitly with synthetic geometry, the way details/T-0141 prescribes.

  The suite runs under `node --test` from a Go test (client_test.go), which
  skips it when node or the jsdom dependency is absent, exactly as the
  check.sh cross-check skips when bash is missing.
*/

'use strict';

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const { JSDOM } = require('jsdom');

const MM_JS = fs.readFileSync(path.join(__dirname, '..', 'static', 'mm.js'), 'utf8');

// The board fixture. Minimal, but faithful to the real render: the app root
// carries data-project-id and the sse/morph extensions; the board, status and
// (absent here) check regions self-describe with hx-get + sse: trigger +
// morph; a card menu with one dialog op, one posting op and one panel op.
const BOARD_HTML = `
<div data-testid="app" data-project-id="x" hx-ext="sse,morph">
  <section data-testid="board" class="mm-board"
           hx-get="/p/x/board?fragment=1" hx-ext="morph"
           hx-trigger="sse:board from:body" hx-swap="morph">
    <section data-testid="board-column-ready" class="mm-column">
      <div data-testid="board-column-ready-body" class="mm-column__body" data-column="ready">
        <article data-testid="item-T-0001" class="mm-item" data-item-id="T-0001" tabindex="0">
          <h3><a href="/p/x/item/T-0001">First</a></h3>
          <button data-testid="item-T-0001-menu" class="mm-item__menu" type="button"
                  aria-expanded="false" aria-label="Actions for T-0001">…</button>
          <div data-testid="item-T-0001-menu-items" class="mm-item__actions" role="menu" hidden>
            <button data-testid="item-T-0001-action-start" class="mm-item__action" role="menuitem"
                    type="button" data-op="start" data-item="T-0001">Start</button>
            <button data-testid="item-T-0001-action-remove" class="mm-item__action" role="menuitem"
                    type="button" data-op="remove" data-item="T-0001">Remove</button>
            <button data-testid="item-T-0001-action-edit" class="mm-item__action" role="menuitem"
                    type="button" data-op="edit" data-item="T-0001">Edit</button>
            <button data-testid="item-T-0001-action-fail" class="mm-item__action" role="menuitem"
                    type="button" disabled data-reason="Conflict" data-op="start" data-item="T-0001">Start</button>
          </div>
        </article>
        <article data-testid="item-T-0002" class="mm-item" data-item-id="T-0002" tabindex="0">
          <h3><a href="/p/x/item/T-0002">Second</a></h3>
        </article>
      </div>
    </section>
    <section data-testid="board-column-done" class="mm-column">
      <div data-testid="board-column-done-body" class="mm-column__body" data-column="done"></div>
    </section>
  </section>
  <footer data-testid="app-status" class="mm-status"
          hx-get="/p/x/status" hx-ext="morph"
          hx-trigger="sse:status from:body" hx-swap="morph">
    <span data-testid="status-wip" class="mm-status__wip">WIP 1/4</span>
  </footer>
  <div data-testid="toast-region" class="mm-toasts"></div>
  <div data-testid="dialog-root" class="mm-dialogs"></div>
</div>
`;

// The same board with a Someday column in front of ready, collapsed the way
// §5.5 renders it: data-collapsed="true", and a body that mm.css hides. jsdom
// applies no stylesheet, so the hiding is not what this fixture reproduces —
// what it reproduces is the consequence, a pointer event that lands on the
// header and never on the body (T-0152).
const COLLAPSED_HTML = BOARD_HTML.replace(
  '<section data-testid="board-column-ready"',
  `<section data-testid="board-column-someday" class="mm-column" data-section="someday"
            data-collapsed="true" data-count="1">
     <header data-testid="board-column-someday-header" class="mm-column__header">
       <button data-testid="board-column-someday-toggle" class="mm-column__toggle"
               type="button" aria-label="Toggle Someday column">v</button>
       <h2 data-testid="board-column-someday-title" class="mm-column__title">Someday</h2>
     </header>
     <div data-testid="board-column-someday-body" class="mm-column__body" data-column="someday">
       <article data-testid="item-T-0009" class="mm-item" data-item-id="T-0009" tabindex="0">
         <h3><a href="/p/x/item/T-0009">Someday one</a></h3>
       </article>
     </div>
   </section>
   <section data-testid="board-column-ready"`,
);

// The board with an item panel over it, carrying the Wake-up group of §5.6 as
// the server renders it (T-0173). The `hidden` attributes are the server's
// first render — the group hidden until the section selector says someday, and
// every control but the selected kind's hidden — and what these tests drive is
// mm.js keeping them right afterwards.
//
// panelHTML(kind, section) builds the panel for a starting state, because both
// visibility rules are about a state CHANGING and the starting point decides
// what a change proves.
function panelHTML(kind = 'never', section = 'ready') {
  const hide = (k) => (k === kind ? '' : 'hidden');
  const panel = `
  <aside data-testid="item-panel" class="mm-panel">
    <form data-testid="item-form">
      <select data-testid="item-field-section" name="section">
        <option value="ready" ${section === 'ready' ? 'selected' : ''}>Ready</option>
        <option value="someday" ${section === 'someday' ? 'selected' : ''}>Someday</option>
      </select>
      <fieldset data-testid="item-tickler" class="mm-tickler"
                data-present="${kind === 'never' ? 'false' : 'true'}"
                ${section === 'someday' ? '' : 'hidden'}>
        <select data-testid="tickler-kind" name="tickler-kind">
          <option value="never" ${kind === 'never' ? 'selected' : ''}>Never</option>
          <option value="one-time" ${kind === 'one-time' ? 'selected' : ''}>One-time</option>
          <option value="weekly" ${kind === 'weekly' ? 'selected' : ''}>Weekly</option>
          <option value="monthly" ${kind === 'monthly' ? 'selected' : ''}>Monthly</option>
        </select>
        <div class="mm-tickler__control" data-kind="one-time" ${hide('one-time')}>
          <input data-testid="tickler-date" type="date" name="tickler-date">
        </div>
        <div class="mm-tickler__control" data-kind="weekly" ${hide('weekly')}>
          <select data-testid="tickler-weekday" name="tickler-weekday"></select>
          <select data-testid="tickler-ordinal" name="tickler-ordinal"></select>
        </div>
        <div class="mm-tickler__control" data-kind="monthly" ${hide('monthly')}>
          <input data-testid="tickler-monthday" type="text" pattern="(0?[1-9]|[12][0-9]|3[01]|last)" name="tickler-monthday">
        </div>
        <div class="mm-tickler__control" data-kind="time" ${kind === 'never' ? 'hidden' : ''}>
          <input data-testid="tickler-time" type="time" name="tickler-time">
        </div>
      </fieldset>
    </form>
  </aside>
`;
  return BOARD_HTML.replace('<div data-testid="toast-region"', panel + '  <div data-testid="toast-region"');
}

// htmx double: records every ajax call, and lets a test fire htmx:* events on
// document.body the way the real htmx does (CustomEvent, bubbles).
function makeHtmx(win) {
  const api = {
    calls: [],
    ajax(method, url, opts) {
      api.calls.push({ method, url, opts });
      // commitMove().catch() needs a promise.
      return Promise.resolve();
    },
    fire(name, detail) {
      const evt = new win.CustomEvent(name, {
        bubbles: true, cancelable: true, composed: true, detail,
      });
      win.document.body.dispatchEvent(evt);
      return evt;
    },
  };
  return api;
}

// load runs the real mm.js against a jsdom document, with the htmx double on
// the global scope. mm.js executes in the NODE realm (with jsdom's document,
// navigator, localStorage as globals) rather than inside the jsdom window, so
// its timers are node's and node:test's mock timers can control them.
function load(t, html = BOARD_HTML, opts = {}) {
  const dom = new JSDOM(html, { url: 'http://localhost/p/x/board' });
  const { window: win } = dom;

  global.window = win;
  global.document = win.document;
  // Node 21+ defines globalThis.navigator as a getter; shadow it.
  Object.defineProperty(global, 'navigator', { value: win.navigator, configurable: true });
  global.localStorage = win.localStorage;
  global.HTMLElement = win.HTMLElement;
  global.HTMLInputElement = win.HTMLInputElement;
  global.CustomEvent = win.CustomEvent;
  global.MouseEvent = win.MouseEvent;
  global.KeyboardEvent = win.KeyboardEvent;
  global.Event = win.Event;

  if (opts.clipboard) {
    win.navigator.clipboard = opts.clipboard;
  }

  const htmx = makeHtmx(win);
  global.htmx = htmx;
  global.__win = win;

  // eslint-disable-next-line no-eval
  (0, eval)(MM_JS);
  return { win, dom, htmx };
}

// Fake dataTransfer, as jsdom events have none (a real drag carries one).
const dragTransfer = () => ({
  effectAllowed: '',
  dropEffect: '',
  setData() {},
});

// A drag event: CustomEvent so we can attach the fake dataTransfer, with the
// coordinates the §7 handlers read.
function dragEvent(win, type, clientY) {
  const evt = new win.CustomEvent(type, { bubbles: true, cancelable: true });
  evt.clientY = clientY;
  Object.defineProperty(evt, 'dataTransfer', { value: dragTransfer(), configurable: true });
  return evt;
}

// Synthetic card geometry: jsdom measures nothing, and the drag index is
// derived from getBoundingClientRect midpoints, so the cards get explicit
// tops. Card i occupies [top, top+height). Column bodies sit at top 0.
function stubLayout(win, height = 40) {
  let top = 0;
  for (const el of win.document.querySelectorAll('.mm-item')) {
    const t = top; // capture THIS card's top; a closure over the live `top`
    // would report the final value for every card.
    el.getBoundingClientRect = () => ({
      top: t, bottom: t + height, height, width: 0, left: 0, right: 0, x: 0, y: t,
      toJSON() {},
    });
    top += height;
  }
  for (const el of win.document.querySelectorAll('.mm-column__body')) {
    el.getBoundingClientRect = () => ({
      top: 0, bottom: 0, height: 0, width: 0, left: 0, right: 0, x: 0, y: 0,
      toJSON() {},
    });
  }
}

const byTestid = (win, id) => win.document.querySelector(`[data-testid="${id}"]`);
const click = (win, el) => el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }));

/* ------------------------------------------------------------------ busy */

test('data-busy tracks requests in flight and settles to false', (t) => {
  const { win, htmx } = load(t);
  const app = byTestid(win, 'app');

  assert.equal(app.getAttribute('data-busy'), 'false', 'born idle');

  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'true', 'one request in flight');

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false', 'settled');

  // Two overlapping requests: busy clears only when BOTH settle.
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'true', 'one of two still in flight');
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false', 'both settled');

  // An errored request settles too.
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:responseError', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false');
});

/* ------------------------------------------------------- echo suppression */

test('a board/status refresh echoes nothing within the mutation window', (t) => {
  const { win, htmx } = load(t);

  // A mutation settles on the board…
  htmx.fire('htmx:afterSettle', {
    requestConfig: { verb: 'patch' },
    target: byTestid(win, 'board'),
  });

  // …and the next board or status GET is cancelled (T-0130).
  const boardGet = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'board'),
  });
  assert.equal(boardGet.defaultPrevented, true, 'board GET during the echo window is cancelled');

  const statusGet = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'app-status'),
  });
  assert.equal(statusGet.defaultPrevented, true, 'status GET during the echo window is cancelled');

  // A GET aimed elsewhere is not the echo.
  const other = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'board-column-ready'),
  });
  assert.equal(other.defaultPrevented, false, 'an unrelated GET is not cancelled');
});

/* ---------------------------------------------------------------- menus */

test('an item menu opens, closes, and closes its siblings', (t) => {
  const { win } = load(t);
  const toggle = byTestid(win, 'item-T-0001-menu');
  const menu = byTestid(win, 'item-T-0001-menu-items');

  assert.equal(menu.hidden, true);
  click(win, toggle);
  assert.equal(menu.hidden, false, 'clicking the toggle opens the menu');
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');

  click(win, toggle);
  assert.equal(menu.hidden, true, 'clicking again closes it');
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
});

test('clicking outside closes every menu', (t) => {
  const { win } = load(t);
  const toggle = byTestid(win, 'item-T-0001-menu');
  const menu = byTestid(win, 'item-T-0001-menu-items');

  click(win, toggle);
  assert.equal(menu.hidden, false);

  click(win, byTestid(win, 'item-T-0002'));
  assert.equal(menu.hidden, true, 'a click on the board closes the menu');
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
});

test('a menu entry posts its operation through htmx.ajax', (t) => {
  const { win, htmx } = load(t);
  const start = byTestid(win, 'item-T-0001-action-start');

  click(win, start);

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0001/start');
  assert.equal(call.opts.target, "[data-testid='board']");
  assert.equal(call.opts.swap, 'morph');
});

test('a dialog op fetches its dialog instead of posting', (t) => {
  const { win, htmx } = load(t);
  click(win, byTestid(win, 'item-T-0001-action-remove'));

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'GET');
  assert.equal(call.url, '/p/x/dialog/confirm-remove?item=T-0001');
  assert.equal(call.opts.target, "[data-testid='dialog-root']");
});

test('panel ops are left to their own hx-get; disabled entries do nothing', (t) => {
  const { win, htmx } = load(t);

  click(win, byTestid(win, 'item-T-0001-action-edit'));
  assert.equal(htmx.calls.length, 0, 'edit opens the panel through the entry\'s own hx-get');

  click(win, byTestid(win, 'item-T-0001-action-fail'));
  assert.equal(htmx.calls.length, 0, 'a disabled entry issues nothing');
});

/* -------------------------------------------------------------- dialogs */

test('Escape closes a dialog and restores focus to its opener', (t) => {
  const { win } = load(t);
  const dialogRoot = byTestid(win, 'dialog-root');
  dialogRoot.innerHTML = '<div class="mm-dialog"><button data-testid="d-inner">ok</button></div>';

  const opener = byTestid(win, 'item-T-0001-menu');
  // The opener is remembered when a request is aimed at the dialog root.
  opener.setAttribute('hx-target', "[data-testid='dialog-root']");
  win.document.body.dispatchEvent(new win.CustomEvent('htmx:beforeRequest', {
    bubbles: true,
    detail: { elt: opener, requestConfig: {} },
  }));

  const dialog = dialogRoot.querySelector('.mm-dialog');
  dialog.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

  assert.equal(dialogRoot.querySelector('.mm-dialog'), null, 'Escape removes the dialog');
  assert.equal(win.document.activeElement, opener, 'focus returns to the opener');
});

test('the focus trap cycles Tab within the dialog', (t) => {
  const { win } = load(t);
  const dialogRoot = byTestid(win, 'dialog-root');
  dialogRoot.innerHTML =
    '<div class="mm-dialog"><button data-testid="d-a">a</button><button data-testid="d-b">b</button></div>';
  const a = byTestid(win, 'd-a');
  const b = byTestid(win, 'd-b');

  b.focus();
  b.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }));
  assert.equal(win.document.activeElement, a, 'Tab from the last wraps to the first');

  a.focus();
  const shifted = new win.KeyboardEvent('keydown', {
    key: 'Tab', shiftKey: true, bubbles: true, cancelable: true,
  });
  a.dispatchEvent(shifted);
  assert.equal(shifted.defaultPrevented, true, 'Shift+Tab from the first is swallowed');
  assert.equal(win.document.activeElement, b, 'Shift+Tab from the first wraps to the last');
});

/* ------------------------------------------------- the Wake-up group (T-0173) */

/*
  §5.6's two visibility rules. Both are client-side because a select must
  answer instantly, and both are only ever a VIEW: what they reveal is still
  composed and validated server-side on save (§4.2), so nothing here decides
  what a schedule means.
*/

const groupOf = (win) => byTestid(win, 'item-tickler');
const controlFor = (win, kind) =>
  groupOf(win).querySelector(`[data-kind="${kind}"]`);
const change = (win, el) => el.dispatchEvent(new win.Event('change', { bubbles: true }));

test('the kind select shows the matching control and hides the others', (t) => {
  const { win } = load(t, panelHTML('never', 'someday'));
  const kind = byTestid(win, 'tickler-kind');

  // Starting state: kind never, so every control is hidden, the time one too.
  for (const k of ['one-time', 'weekly', 'monthly', 'time']) {
    assert.equal(controlFor(win, k).hidden, true, `${k} starts hidden`);
  }

  kind.value = 'weekly';
  change(win, kind);
  assert.equal(controlFor(win, 'weekly').hidden, false, 'weekly is revealed');
  assert.equal(controlFor(win, 'one-time').hidden, true, 'one-time stays hidden');
  assert.equal(controlFor(win, 'monthly').hidden, true, 'monthly stays hidden');
  assert.equal(controlFor(win, 'time').hidden, false, 'the time control shows for every kind but never');

  kind.value = 'monthly';
  change(win, kind);
  assert.equal(controlFor(win, 'monthly').hidden, false, 'monthly is revealed');
  assert.equal(controlFor(win, 'weekly').hidden, true, 'the previous kind is hidden again');

  kind.value = 'never';
  change(win, kind);
  for (const k of ['one-time', 'weekly', 'monthly', 'time']) {
    assert.equal(controlFor(win, k).hidden, true, `${k} is hidden again by never`);
  }
});

test('the new panel reveals the group when the section becomes Someday', (t) => {
  const { win } = load(t, panelHTML('never', 'ready'));
  const section = byTestid(win, 'item-field-section');

  assert.equal(groupOf(win).hidden, true, 'the selector defaults to Ready, where the group is hidden');

  section.value = 'someday';
  change(win, section);
  assert.equal(groupOf(win).hidden, false, 'Someday reveals it without a round trip');

  section.value = 'ready';
  change(win, section);
  assert.equal(groupOf(win).hidden, true, 'and switching back hides it again');
});

test('a swap re-reconciles the group with its kind select', (t) => {
  // save-and-add-another replaces the panel wholesale, so the settle handler is
  // what keeps a freshly swapped form's controls matching its own select.
  const { win, htmx } = load(t, panelHTML('never', 'someday'));
  const kind = byTestid(win, 'tickler-kind');

  // A swap lands a form whose select says one-time while the controls still
  // carry the previous render's hidden flags.
  kind.value = 'one-time';
  assert.equal(controlFor(win, 'one-time').hidden, true, 'stale, as a swap can leave it');

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(controlFor(win, 'one-time').hidden, false, 'the settle reconciles it');
  assert.equal(controlFor(win, 'time').hidden, false, 'and the time control with it');
});

test('a board with no Wake-up group is left alone', (t) => {
  // The group is absent from a ready or blocked item's panel, and from the
  // board on its own. Neither handler may throw when it finds nothing.
  const { win, htmx } = load(t);
  const section = win.document.createElement('select');
  section.setAttribute('data-testid', 'item-field-section');
  win.document.body.appendChild(section);

  change(win, section);
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(byTestid(win, 'item-tickler'), null, 'nothing was invented');
});

/* ---------------------------------------------------------------- toasts */

test('toasts auto-dismiss after six seconds, unless test mode', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const { win, htmx } = load(t);
  const region = byTestid(win, 'toast-region');
  region.innerHTML = '<div data-testid="toast-1" class="mm-toast">done</div>';

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'post' } });
  assert.ok(byTestid(win, 'toast-1'), 'the toast is still there at first');
  assert.equal(byTestid(win, 'toast-1').getAttribute('data-timed'), 'true');

  t.mock.timers.tick(6000);
  assert.equal(byTestid(win, 'toast-1'), null, 'after six seconds it is gone');
});

test('test mode keeps toasts until dismissed', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const { win, htmx } = load(t);
  byTestid(win, 'app').setAttribute('data-test-mode', 'true');
  const region = byTestid(win, 'toast-region');
  region.innerHTML = '<div data-testid="toast-1" class="mm-toast">done</div>';

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'post' } });
  t.mock.timers.tick(6000);
  assert.ok(byTestid(win, 'toast-1'), 'test mode does not auto-dismiss (§4.4)');
});

/* ---------------------------------------------------------------- copy */

test('copy puts the data-clipboard text on the clipboard', async (t) => {
  let written = null;
  const { win } = load(t, BOARD_HTML, {
    clipboard: { writeText: (text) => { written = text; return Promise.resolve(); } },
  });
  const button = win.document.createElement('button');
  button.setAttribute('data-clipboard', '- [ ] [T-0001] First');
  button.setAttribute('data-testid', 'x-copy');
  win.document.body.appendChild(button);

  click(win, button);
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(written, '- [ ] [T-0001] First');
  assert.equal(button.getAttribute('data-copied'), 'true');
});

/* ------------------------------------------------------------ drag & drop */

test('dragstart marks the card and the source column', (t) => {
  const { win } = load(t);
  const card = byTestid(win, 'item-T-0002');
  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  assert.equal(card.getAttribute('data-dragging'), 'true');
  assert.equal(byTestid(win, 'app').getAttribute('data-drag-source'), 'board-column-ready-body');
});

test('dragover sets the drop index from the pointer, not the card', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  // T-0002 is the dragged card; T-0001 occupies [0,40). clientY=10 is above
  // its midpoint, so the insertion index is 0.
  body.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(column.getAttribute('data-drop-index'), '0');
  assert.equal(column.getAttribute('data-drop-target'), 'true');
  assert.equal(column.getAttribute('data-drop-allowed'), 'true');

  // Below T-0001's midpoint: index 1 (append).
  body.dispatchEvent(dragEvent(win, 'dragover', 30));
  assert.equal(column.getAttribute('data-drop-index'), '1');

  // A placeholder marks the insertion point, allowed or not.
  assert.ok(win.document.querySelector('[data-testid="drop-placeholder"]'), 'a placeholder is present');
  assert.equal(
    win.document.querySelector('[data-testid="drop-placeholder"]').getAttribute('data-drop-allowed'),
    'true',
  );
});

test('drop commits with the drop\'s own coordinates, not the last dragover', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  body.dispatchEvent(dragEvent(win, 'dragover', 10)); // index 0…
  body.dispatchEvent(dragEvent(win, 'drop', 30));     // …but the release is at 1

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0002/move');
  assert.equal(call.opts.values.position, '2', 'the drop position comes from the release');
  assert.equal(call.opts.values.section, 'ready');
  assert.equal(call.opts.swap, 'morph');
});

test('dragging into a column makes the column the target and clears the others', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const ready = byTestid(win, 'board-column-ready');
  const done = byTestid(win, 'board-column-done');
  const doneBody = byTestid(win, 'board-column-done-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  ready.setAttribute('data-drop-index', '1');
  doneBody.dispatchEvent(dragEvent(win, 'dragover', 10));

  assert.equal(ready.getAttribute('data-drop-index'), null, 'the old target is cleared');
  assert.equal(done.getAttribute('data-drop-index'), '0');
  assert.equal(done.getAttribute('data-drop-allowed'), 'true');
});

/* ------------------------------------------------- collapsed Someday (T-0152) */

test('a collapsed column is a drop target through its header', (t) => {
  const { win } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const someday = byTestid(win, 'board-column-someday');
  const title = byTestid(win, 'board-column-someday-title');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  // The pointer is over the rotated title — the body is hidden, so this is the
  // only thing a collapsed column offers to aim at.
  const over = dragEvent(win, 'dragover', 10);
  title.dispatchEvent(over);

  assert.equal(over.defaultPrevented, true, 'the dragover is accepted, so a drop can follow');
  assert.equal(someday.getAttribute('data-drop-target'), 'true');
  assert.equal(someday.getAttribute('data-drop-allowed'), 'true');
  // One card already in someday, and a collapsed drop goes to the bottom.
  assert.equal(someday.getAttribute('data-drop-index'), '1', 'collapsed drops land at the bottom');
});

test('dropping on a collapsed column moves the item to the bottom of it', (t) => {
  const { win, htmx } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const header = byTestid(win, 'board-column-someday-header');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  header.dispatchEvent(dragEvent(win, 'dragover', 10));
  header.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1, 'the drop posts the move');
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0002/move');
  assert.equal(call.opts.values.section, 'someday');
  assert.equal(call.opts.values.position, '2', 'after the one card already there');
});

test('a keyboard move into a collapsed column also lands at the bottom', (t) => {
  const { win, htmx } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  // T-0001 is FIRST in ready, so the index it carries across is 0 — which is
  // the bottom only if the collapsed rule does nothing.
  const card = byTestid(win, 'item-T-0001');
  const someday = byTestid(win, 'board-column-someday');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  // someday is the leftmost column, ready the next: one ArrowLeft crosses.
  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
  assert.equal(someday.getAttribute('data-drop-index'), '1', 'the index is the bottom, not the carried one');

  // Arrows within a collapsed column cannot aim: there is nothing to see.
  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
  assert.equal(someday.getAttribute('data-drop-index'), '1');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert.equal(htmx.calls.length, 1);
  assert.equal(htmx.calls[0].opts.values.position, '2');
  assert.equal(htmx.calls[0].opts.values.section, 'someday');
});

test('an expanded column still takes its index from the pointer', (t) => {
  // The collapsed rule must not leak into the normal case: with
  // data-collapsed="false" the body is in the layout and §7.3's measured index
  // stands.
  const { win } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const someday = byTestid(win, 'board-column-someday');
  someday.setAttribute('data-collapsed', 'false');
  const body = byTestid(win, 'board-column-someday-body');
  const card = byTestid(win, 'item-T-0002');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  // T-0009 is stubbed at [0,40); a pointer above its midpoint inserts before it.
  body.dispatchEvent(dragEvent(win, 'dragover', 5));
  assert.equal(someday.getAttribute('data-drop-index'), '0');
});

test('dragging into done prompts the finish dialog', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const doneBody = byTestid(win, 'board-column-done-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  doneBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  doneBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'GET');
  assert.equal(call.url, '/p/x/dialog/finish?item=T-0002', 'finish prompts before it runs (§7.2)');
});

test('an illegal drop issues no request', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');

  // working -> working is Conflict, so make the source column a working one
  // and drop within it.
  body.setAttribute('data-column', 'working');
  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  body.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(byTestid(win, 'board-column-ready').getAttribute('data-drop-allowed'), 'false');
  body.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 0, 'a Conflict drop posts nothing');
});

test('dragend clears every drag attribute', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  body.dispatchEvent(dragEvent(win, 'dragover', 10));
  card.dispatchEvent(dragEvent(win, 'dragend', 10));

  assert.equal(card.getAttribute('data-dragging'), null);
  assert.equal(byTestid(win, 'app').getAttribute('data-drag-source'), null);
  assert.equal(column.getAttribute('data-drop-index'), null);
  assert.equal(win.document.querySelector('[data-testid="drop-placeholder"]'), null);
});

/* ---------------------------------------------------------- keyboard moves */

test('keyboard move mode: space enters, arrows move, enter commits', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  assert.equal(card.getAttribute('data-move-mode'), 'true', 'space enters move mode');
  assert.equal(column.getAttribute('data-drop-index'), '1', 'begins at the card\'s index');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
  assert.equal(column.getAttribute('data-drop-index'), '0', 'arrow up moves the index');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert.equal(htmx.calls.length, 1, 'enter commits');
  assert.equal(htmx.calls[0].url, '/p/x/items/T-0002/move');
  assert.equal(htmx.calls[0].opts.values.position, '1');
  assert.equal(card.getAttribute('data-move-mode'), null, 'commit ends move mode');
});

test('keyboard move mode: escape cancels and restores focus', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  const esc = new win.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  card.dispatchEvent(esc);

  assert.equal(esc.defaultPrevented, true);
  assert.equal(card.getAttribute('data-move-mode'), null);
  assert.equal(win.document.activeElement, card, 'focus returns to the card');
});

/* ---------------------------------------------------- SSE-down polling (T-0139) */

test('the stream closing starts the 5s poll; reopening stops it', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  htmx.fire('htmx:sseError');
  assert.equal(htmx.calls.length, 2, 'polling converges immediately on stream-down');
  const urls = htmx.calls.map((c) => c.url).sort();
  assert.deepEqual(urls, ['/p/x/board?fragment=1', '/p/x/status']);
  for (const call of htmx.calls) {
    assert.equal(call.method, 'GET');
    assert.equal(call.opts.swap, 'morph', 'polls morph their own region');
  }

  t.mock.timers.tick(5000);
  assert.equal(htmx.calls.length, 4, 'one interval tick polls both regions again');

  htmx.fire('htmx:sseOpen');
  t.mock.timers.tick(15000);
  assert.equal(htmx.calls.length, 4, 'reopening the stream stops the poll for good');
});

test('sseClose starts the poll; a second close does not stack intervals', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  htmx.fire('htmx:sseClose');
  htmx.fire('htmx:sseClose');
  htmx.fire('htmx:sseError');
  assert.equal(htmx.calls.length, 2, 'every close/error is guarded by one interval');

  t.mock.timers.tick(5000);
  assert.equal(htmx.calls.length, 4, 'exactly one interval runs');
});

test('the theme shell is not polled', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  // The app root self-refreshes the whole shell on sse:theme with outerHTML.
  // It must NOT join the poll loop: a full app re-render every 5s is the very
  // waste T-0139 exists to avoid.
  byTestid(win, 'app').setAttribute('hx-trigger', 'sse:theme from:body');
  byTestid(win, 'app').setAttribute('hx-get', '/p/x/shell');

  htmx.fire('htmx:sseError');
  t.mock.timers.tick(5000);

  assert.ok(htmx.calls.every((c) => c.url !== '/p/x/shell'), 'the shell is not polled');
  assert.equal(htmx.calls.length, 4, 'only the board and status regions poll');
});
