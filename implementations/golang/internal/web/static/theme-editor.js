/*
  micro-manager — the theme editor's client behaviour.

  Deliberately a separate file from mm.js: the app shell's script has a size
  guard (it must stay under 700 lines; see dragdrop_test.go), and the editor's
  preview is page-specific. It loads only on /settings/theme, via the script
  tag in the theme-editor template.

  What it does, and why it is allowed to exist at all:

  1. Live preview. §8.7: "Editing a theme MUST apply live to the current view,
     so the editor is a preview." The server renders the resolved theme as
     custom properties on the app root; typing in a token input overrides the
     matching property in place, so the page itself is the preview — the
     header, the nav, the editor and the status bar all re-theme as the user
     types. The server stays authoritative exactly as §2.1 requires: the
     preview is ephemeral (a reload drops it, because the next render starts
     from the theme files again), and Save posts the form values the user can
     see, which the server validates before it writes.

  2. Appearance preview. The dark palette rides on the editor container as
     data-dark-tokens (the RESOLVED values, serialised by the server), so the
     dark half is visible even when the OS is light.

  3. Palette from a colour (T-0137). The derivation lives in the library
     (mm.HarmonizedPalette); /settings/theme/palette hands the result over as
     JSON, and the client fills the form — which is also what makes the
     preview live, because filling an input fires the same input listener the
     typing preview uses. The dark half is stored for the appearance preview
     and is not saved: the editor has no colorDark fields, and the save
     handler preserves the resolved dark palette instead.
*/
(function () {
  'use strict';

  const root = () => document.querySelector('[data-testid="app"]');
  const editor = () => document.querySelector('.mm-theme-editor');

  function tokenPathToProperty(path) {
    // color.accent.base -> --mm-color-accent-base; gui.space.3 -> --mm-space-3.
    // The same mapping as mm.CSSPropertyName, kept here because the mapping is
    // the one piece the server cannot hand the client without a round trip.
    const p = path.startsWith('gui.') ? path.slice(4) : path;
    return '--mm-' + p.toLowerCase().replace(/\./g, '-');
  }

  function applyToken(path, value) {
    const app = root();
    if (!app || !path) return;
    const name = tokenPathToProperty(path);
    if (value === '') app.style.removeProperty(name);
    else app.style.setProperty(name, value);
  }

  function updateSwatch(input) {
    const swatch = document.querySelector(`[data-swatch-for="${input.id}"]`);
    if (swatch) swatch.style.backgroundColor = input.value;
  }

  document.body.addEventListener('input', (event) => {
    const input = event.target;
    if (!input.matches || !input.matches('[data-token-path]')) return;
    applyToken(input.getAttribute('data-token-path'), input.value);
    updateSwatch(input);
  });

  /* -------------------------------------------------- appearance preview */

  /*
    When the user picks dark or auto, a small style block forces the dark
    palette onto the app root so it is visible even on a light OS. Choosing
    light drops the block again. The OS media query for auto is the one thing
    a preview cannot fight without !important; light-on-dark-OS is the rarer
    half of the pair and is left to the OS.
  */
  const previewDarkId = 'mm-preview-dark';

  function previewAppearance(appearance) {
    const app = root();
    const ed = editor();
    const prev = document.getElementById(previewDarkId);
    if (prev) prev.remove();
    if (!app || !ed || (appearance !== 'dark' && appearance !== 'auto')) return;

    let darkTokens = null;
    try {
      darkTokens = JSON.parse(ed.getAttribute('data-dark-tokens') || '{}');
    } catch (_) {}
    if (!darkTokens || Object.keys(darkTokens).length === 0) return;

    let css = '[data-testid="app"]{';
    for (const [token, value] of Object.entries(darkTokens)) {
      css += tokenPathToProperty('color.' + token) + ':' + value + ';';
    }
    css += '}';
    const style = document.createElement('style');
    style.id = previewDarkId;
    style.textContent = css;
    ed.appendChild(style);
  }

  document.body.addEventListener('change', (event) => {
    if (event.target.id !== 'theme-appearance') return;
    previewAppearance(event.target.value);
  });

  /* ------------------------------------------------- palette from a colour */

  function fillPalette(light, dark) {
    const ed = editor();
    if (!ed) return;
    ed.setAttribute('data-dark-tokens', JSON.stringify(dark));
    for (const [token, value] of Object.entries(light)) {
      const input = document.querySelector(`[data-token-path="color.${token}"]`);
      if (!input) continue;
      input.value = value;
      applyToken('color.' + token, value);
      updateSwatch(input);
    }
  }

  document.body.addEventListener('click', (event) => {
    const button = event.target.closest('[data-testid="x-palette-generate"]');
    if (!button) return;
    const base = document.querySelector('[data-testid="x-palette-base"]');
    const message = document.querySelector('[data-testid="x-palette-message"]');
    const value = (base && base.value.trim()) || '';
    if (message) message.textContent = '';
    if (!value) {
      if (message) message.textContent = 'Type a colour first, e.g. #1257c9.';
      if (base) base.focus();
      return;
    }

    fetch('/settings/theme/palette?base=' + encodeURIComponent(value))
      .then((response) => {
        if (!response.ok) throw new Error('the server rejected the colour');
        return response.json();
      })
      .then((data) => {
        fillPalette(data.light, data.dark);
        const appearance = document.getElementById('theme-appearance');
        if (appearance) previewAppearance(appearance.value);
      })
      .catch(() => {
        if (message) message.textContent = 'Not a colour the theme accepts (#rrggbb or #rrggbbaa).';
      });
  });
})();
