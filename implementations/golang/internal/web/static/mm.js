/*
  micro-manager — the only hand-written JavaScript in the project.

  It contains three things and no domain logic beyond one deliberate exception:

  1. data-busy tracking (spec-gui.md §5.1) — the quiescence signal the external
     suite waits on.
  2. Menus and dialogs: opening, closing, and trapping focus (§5.10, §11 rule 5).
  3. Drag and drop with its keyboard equivalent (§7) — added by T-0059.

  The exception is the static transition table of §7.2, replicated so hover
  feedback is instant. §2.1 permits it explicitly, provided the server's answer
  stays authoritative: the table decides what the cursor looks like, and the
  server decides whether the drop happens.

  Keep this file small. If it grows past a few hundred lines, something belongs
  on the server that has drifted onto the client.
*/
(function () {
  'use strict';

  const root = () => document.querySelector('[data-testid="app"]');
  const dialogRoot = () => document.querySelector('[data-testid="dialog-root"]');

  /* ------------------------------------------------------------ data-busy */

  /*
    data-busy is true while ANY request is in flight and false only when the
    view fully reflects server state (§5.1). Requests are counted rather than
    flagged: two overlapping swaps would otherwise clear it while one is still
    running, and every test that waits on quiescence would race.
  */
  let inFlight = 0;

  function setBusy() {
    const app = root();
    if (app) app.setAttribute('data-busy', inFlight > 0 ? 'true' : 'false');
  }

  document.body.addEventListener('htmx:beforeRequest', () => {
    inFlight += 1;
    setBusy();
  });

  for (const event of ['htmx:afterSettle', 'htmx:responseError', 'htmx:sendError', 'htmx:timeout']) {
    document.body.addEventListener(event, () => {
      inFlight = Math.max(0, inFlight - 1);
      setBusy();
    });
  }

  /* ---------------------------------------------------------------- menus */

  /*
    An item's action menu. The button carries aria-expanded, which is the state
    an assistive technology reads and a test can assert without measuring
    anything.
  */
  function closeMenus(except) {
    document.querySelectorAll('[data-testid$="-menu-items"]').forEach((menu) => {
      if (menu === except) return;
      menu.hidden = true;
      const button = menu.previousElementSibling;
      if (button && button.hasAttribute('aria-expanded')) {
        button.setAttribute('aria-expanded', 'false');
      }
    });
  }

  document.addEventListener('click', (event) => {
    const toggle = event.target.closest('[data-testid$="-menu"], [data-testid="project-switcher"]');

    if (toggle && toggle.getAttribute('data-testid') === 'project-switcher') {
      const menu = document.querySelector('[data-testid="project-switcher-menu"]');
      const open = menu.hidden;
      menu.hidden = !open;
      toggle.setAttribute('aria-expanded', String(open));
      return;
    }

    if (toggle) {
      const menu = toggle.nextElementSibling;
      if (menu) {
        const open = menu.hidden;
        closeMenus(menu);
        menu.hidden = !open;
        toggle.setAttribute('aria-expanded', String(open));
      }
      return;
    }

    if (event.target.closest('[data-dismiss]')) {
      const dialog = event.target.closest('.mm-dialog');
      const toast = event.target.closest('.mm-toast');
      if (dialog) closeDialog(dialog);
      else if (toast) toast.remove();
      return;
    }

    if (!event.target.closest('.mm-item__actions, .mm-dialog, [data-testid="project-switcher-menu"]')) {
      closeMenus(null);
      const switcher = document.querySelector('[data-testid="project-switcher-menu"]');
      if (switcher) {
        switcher.hidden = true;
        document.querySelector('[data-testid="project-switcher"]')
          ?.setAttribute('aria-expanded', 'false');
      }
    }
  });

  /* -------------------------------------------------------------- dialogs */

  /*
    §11 rule 5: dialogs trap focus and restore it to the invoking element on
    close. The invoking element is remembered here rather than guessed on close,
    because by then the button may have been replaced by a swap.
  */
  let dialogOpener = null;

  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const target = event.detail && event.detail.elt;
    if (target && target.getAttribute && target.getAttribute('hx-target') === "[data-testid='dialog-root']") {
      dialogOpener = target;
    }
  });

  document.body.addEventListener('htmx:afterSettle', () => {
    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (dialog) focusFirst(dialog);
  });

  function focusFirst(dialog) {
    const focusable = dialog.querySelector(
      'input:not([type=hidden]), select, textarea, button, [href]'
    );
    if (focusable) focusable.focus();
  }

  function closeDialog(dialog) {
    dialog.remove();
    if (dialogOpener && document.contains(dialogOpener)) {
      dialogOpener.focus();
    }
    dialogOpener = null;
  }

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;

    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (dialog) {
      closeDialog(dialog);
      return;
    }
    closeMenus(null);
  });

  /* Focus trap: Tab cycles within an open dialog rather than escaping behind it. */
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (!dialog) return;

    const focusable = Array.from(
      dialog.querySelectorAll('input:not([type=hidden]), select, textarea, button, [href]')
    ).filter((el) => !el.disabled);
    if (focusable.length === 0) return;

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  /* ----------------------------------------------------------- item menus */

  /*
    A menu entry posts its operation. The disabled ones carry data-reason and do
    nothing at all — the server would refuse them with the same code, and issuing
    a request that is known to fail would only make the failure slower.
  */
  document.addEventListener('click', (event) => {
    const button = event.target.closest('.mm-item__action');
    if (!button || button.disabled) return;

    const op = button.getAttribute('data-op');
    const id = button.getAttribute('data-item');
    const app = root();
    if (!op || !id || !app) return;

    const project = app.getAttribute('data-project-id');
    if (!project) return;

    // The three operations that prompt first (§7.2). Everything else posts.
    if (op === 'remove' || op === 'block' || op === 'finish') {
      const name = op === 'remove' ? 'confirm-remove' : op;
      dialogOpener = button;
      htmx.ajax('GET', `/p/${project}/dialog/${name}?item=${id}`, {
        target: "[data-testid='dialog-root']",
      });
      return;
    }
    if (op === 'edit' || op === 'move') return; // the panel owns these

    htmx.ajax('POST', `/p/${project}/items/${id}/${op}`, {
      target: "[data-testid='board']",
      swap: 'outerHTML',
    });
  });

  /* ---------------------------------------------------------------- copy */

  /*
    §5.7: report-copy puts the paste-ready markdown on the clipboard. The text
    comes from the DOM rather than from a second render, so what is copied is
    what the server produced - and what the CLI produces, since both call the
    library's one renderer.
  */
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-clipboard]');
    if (!button) return;

    const text = button.getAttribute('data-clipboard');
    const done = () => {
      button.setAttribute('data-copied', 'true');
      setTimeout(() => button.removeAttribute('data-copied'), 2000);
    };

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, () => fallbackCopy(text, done));
      return;
    }
    fallbackCopy(text, done);
  });

  /* Clipboard access can be refused, and a copy button that silently does
     nothing is worse than one that uses the old API. */
  function fallbackCopy(text, done) {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'absolute';
    area.style.left = '-9999px';
    document.body.appendChild(area);
    area.select();
    try {
      document.execCommand('copy');
      done();
    } finally {
      area.remove();
    }
  }

  /* Toasts persist in test mode until dismissed (§4.4 rule 2). */
  document.body.addEventListener('htmx:afterSettle', () => {
    const app = root();
    if (!app || app.getAttribute('data-test-mode') === 'true') return;
    document.querySelectorAll('.mm-toast:not([data-timed])').forEach((toast) => {
      toast.setAttribute('data-timed', 'true');
      setTimeout(() => toast.remove(), 6000);
    });
  });

  /* ================================================================ §7 */

  /*
    Drag and drop, and the keyboard move mode.

    §7.4 is the reason these are one mechanism rather than two: "Move mode MUST
    maintain the same data-drop-* attributes as a pointer drag, so one set of
    test assertions covers both input paths." Everything below therefore goes
    through beginMove / hoverTarget / commitMove, and the pointer handlers and
    the key handlers are two thin ways in.
  */

  /*
    The transition table of §7.2, replicated here so hover feedback is instant.

    This is the ONE domain rule the client is allowed to hold (§2.1), and only
    because the server's answer stays authoritative: the table decides what the
    cursor looks like, and the server decides whether the drop happens. The
    error codes are the ones the server would return, so the two agree.
  */
  function legality(from, to) {
    const backlog = (k) => k === 'ready' || k === 'blocked' || k === 'someday';

    if (from === 'working' && to === 'working') return { allowed: false, reason: 'Conflict' };
    if (from === to) return { allowed: true, op: 'move' };
    if (to === 'done') return { allowed: true, op: 'finish' };
    if (from === 'done') return { allowed: false, reason: 'Conflict' };
    if (backlog(from) && to === 'working') return { allowed: true, op: 'start' };
    if (from === 'working' && backlog(to)) return { allowed: true, op: 'pause' };
    if (backlog(from) && to === 'blocked') return { allowed: true, op: 'block' };
    if (from === 'blocked' && backlog(to)) return { allowed: true, op: 'unblock' };
    if (backlog(from) && backlog(to)) return { allowed: true, op: 'move' };
    return { allowed: false, reason: 'Conflict' };
  }

  /* The move in progress: pointer or keyboard, one shape either way. */
  let move = null;

  const columnKey = (body) => body.getAttribute('data-column');
  const columnBodies = () => Array.from(document.querySelectorAll('.mm-column__body'));
  const cardsIn = (body) => Array.from(body.querySelectorAll('.mm-item'));

  function announce(text) {
    const board = document.querySelector('[data-testid="board"]');
    if (board) board.setAttribute('aria-label', text);
  }

  function beginMove(card, keyboard) {
    const body = card.closest('.mm-column__body');
    if (!body) return;

    move = {
      card,
      from: columnKey(body),
      fromBody: body,
      index: cardsIn(body).indexOf(card),
      keyboard: Boolean(keyboard),
      target: body,
      /* The pre-drag DOM, restored verbatim if the server refuses (§7.1). */
      nextSibling: card.nextElementSibling,
    };

    card.setAttribute('data-dragging', 'true');
    root().setAttribute('data-drag-source', `board-column-${move.from}-body`);
    if (keyboard) card.setAttribute('data-move-mode', 'true');

    hoverTarget(body, move.index);
    announce(`Moving ${card.getAttribute('data-item-id')}`);
  }

  /* hoverTarget maintains every attribute §7.3 fixes, on every move. */
  function hoverTarget(body, index) {
    if (!move) return;

    for (const other of columnBodies()) {
      const column = other.closest('.mm-column');
      if (other === body) continue;
      column.removeAttribute('data-drop-target');
      column.removeAttribute('data-drop-allowed');
      column.removeAttribute('data-drop-index');
      column.removeAttribute('data-drop-reason');
    }
    document.querySelectorAll('[data-testid="drop-placeholder"]').forEach((p) => p.remove());

    const column = body.closest('.mm-column');
    const to = columnKey(body);
    const verdict = legality(move.from, to);
    const cards = cardsIn(body).filter((c) => c !== move.card);
    const clamped = Math.max(0, Math.min(index, cards.length));

    move.target = body;
    move.index = clamped;
    move.op = verdict.op;
    move.allowed = verdict.allowed;

    column.setAttribute('data-drop-target', 'true');
    column.setAttribute('data-drop-allowed', verdict.allowed ? 'true' : 'false');
    column.setAttribute('data-drop-index', String(clamped));
    if (verdict.reason) column.setAttribute('data-drop-reason', verdict.reason);
    else column.removeAttribute('data-drop-reason');

    /* The placeholder marks the insertion point (§7.3). */
    const placeholder = document.createElement('div');
    placeholder.setAttribute('data-testid', 'drop-placeholder');
    placeholder.className = 'mm-drop-placeholder';
    placeholder.setAttribute('data-drop-allowed', verdict.allowed ? 'true' : 'false');
    if (clamped >= cards.length) body.appendChild(placeholder);
    else body.insertBefore(placeholder, cards[clamped]);

    announce(`${move.card.getAttribute('data-item-id')} to ${to}, position ${clamped + 1}`);
  }

  function endMove() {
    if (!move) return;
    move.card.removeAttribute('data-dragging');
    move.card.removeAttribute('data-move-mode');
    root().removeAttribute('data-drag-source');
    document.querySelectorAll('[data-testid="drop-placeholder"]').forEach((p) => p.remove());
    document.querySelectorAll('.mm-column').forEach((column) => {
      column.removeAttribute('data-drop-target');
      column.removeAttribute('data-drop-allowed');
      column.removeAttribute('data-drop-index');
      column.removeAttribute('data-drop-reason');
    });
    move = null;
  }

  /*
    Commit. An illegal target issues NO REQUEST (§7.2), and a legal one goes
    through htmx.ajax so the response flows through the same swap and
    out-of-band machinery as every other mutation - no parallel update path.
  */
  function commitMove() {
    if (!move) return;
    if (!move.allowed) {
      announce('That move is not allowed');
      endMove();
      return;
    }

    const app = root();
    const project = app.getAttribute('data-project-id');
    const id = move.card.getAttribute('data-item-id');
    const to = columnKey(move.target);
    const op = move.op;
    const index = move.index;
    const card = move.card;

    endMove();

    /* Operations that must prompt before they run (§7.2). Cancelling aborts. */
    if (op === 'block' || op === 'finish' || (op === 'pause' && to === 'blocked')) {
      dialogOpener = card;
      const dialogName = (op === 'pause' && to === 'blocked') ? 'block' : op;
      htmx.ajax('GET', `/p/${project}/dialog/${dialogName}?item=${id}`, {
        target: "[data-testid='dialog-root']",
      });
      return;
    }

    const values = { position: String(index + 1) };
    if (op === 'move' || op === 'pause') {
      /* Which column it landed in. A pause names it because §7.2 takes the
         position from the drop index; a move within the backlog names it
         because the section may have changed. */
      values.section = to;
    }

    htmx.ajax('POST', `/p/${project}/items/${id}/${op}`, {
      target: "[data-testid='board']",
      swap: 'outerHTML',
      values,
    }).catch(() => {
      /* A rejected drop restores the pre-drag DOM: the board is re-rendered
         from the server on success, and left untouched on failure, so nothing
         optimistic has to be unwound. §7.1 permits an optimistic update only
         when it can be fully reverted - not doing one is the simpler way to
         satisfy that. */
      announce('The move was refused');
    });
  }

  /* ------------------------------------------------------- pointer drags */

  document.addEventListener('dragstart', (event) => {
    const card = event.target.closest('.mm-item');
    if (!card) return;
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', card.getAttribute('data-item-id'));
    beginMove(card, false);
  });

  document.addEventListener('dragover', (event) => {
    if (!move) return;
    const body = event.target.closest('.mm-column__body');
    if (!body) return;
    event.preventDefault();

    /* The insertion index is where the pointer is, not where the card is. */
    const cards = cardsIn(body).filter((c) => c !== move.card);
    let index = cards.length;
    for (let i = 0; i < cards.length; i += 1) {
      const box = cards[i].getBoundingClientRect();
      if (event.clientY < box.top + box.height / 2) {
        index = i;
        break;
      }
    }
    hoverTarget(body, index);
    event.dataTransfer.dropEffect = move.allowed ? 'move' : 'none';
  });

  document.addEventListener('drop', (event) => {
    if (!move) return;
    event.preventDefault();
    commitMove();
  });

  document.addEventListener('dragend', () => endMove());

  /* ------------------------------------------------------ keyboard moves */

  /*
    §7.4, required and normative rather than an accessibility afterthought.
    Space or Enter enters move mode, arrows change index and column, Enter
    commits, Escape cancels and restores focus and position.
  */
  document.addEventListener('keydown', (event) => {
    const card = event.target.closest && event.target.closest('.mm-item');

    if (!move) {
      if (!card) return;
      if (event.key === ' ' || event.key === 'Enter') {
        event.preventDefault();
        beginMove(card, true);
      }
      return;
    }

    const bodies = columnBodies();
    const current = bodies.indexOf(move.target);

    switch (event.key) {
      case 'ArrowUp':
        event.preventDefault();
        hoverTarget(move.target, move.index - 1);
        break;
      case 'ArrowDown':
        event.preventDefault();
        hoverTarget(move.target, move.index + 1);
        break;
      case 'ArrowLeft':
        event.preventDefault();
        hoverTarget(bodies[Math.max(0, current - 1)], move.index);
        break;
      case 'ArrowRight':
        event.preventDefault();
        hoverTarget(bodies[Math.min(bodies.length - 1, current + 1)], move.index);
        break;
      case 'Enter':
        event.preventDefault();
        commitMove();
        break;
      case 'Escape': {
        event.preventDefault();
        const focus = move.card;
        endMove();
        focus.focus();
        break;
      }
      default:
        break;
    }
  });

  setBusy();
})();
