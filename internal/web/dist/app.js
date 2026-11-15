const BASE = document.querySelector('meta[name="base"]')?.content || '/';
const API = (path) => BASE + String(path).replace(/^\/+/, '');
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

const PRESETS = {
  blackflame: {
    bg: '#101214', surface: '#181b1e', surface2: '#22262a', text: '#eef0f2',
    muted: '#9ba2aa', accent: '#efb66c', accent2: '#f4d29f', border: '#30353a',
    radius: '16px', shadow: '0 18px 60px rgba(0, 0, 0, 0.35)', tileSize: '168px',
    font: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },
  light: {
    bg: '#f5f2ea', surface: '#fffdf8', surface2: '#f0ece2', text: '#1d1a16',
    muted: '#6b645b', accent: '#d94a2b', accent2: '#e79a2d', border: '#d8d2c5',
    radius: '16px', shadow: '0 18px 50px rgba(80, 62, 40, 0.16)', tileSize: '148px',
    font: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },
  nord: {
    bg: '#2e3440', surface: '#3b4252', surface2: '#434c5e', text: '#eceff4',
    muted: '#aab3c5', accent: '#88c0d0', accent2: '#ebcb8b', border: '#4c566a',
    radius: '16px', shadow: '0 18px 60px rgba(0, 0, 0, 0.3)', tileSize: '148px',
    font: 'Inter, ui-sans-serif, system-ui, sans-serif',
  },
  gruvbox: {
    bg: '#1d2021', surface: '#282828', surface2: '#32302f', text: '#ebdbb2',
    muted: '#a89984', accent: '#fe8019', accent2: '#fabd2f', border: '#3c3836',
    radius: '14px', shadow: '0 18px 60px rgba(0, 0, 0, 0.4)', tileSize: '148px',
    font: 'Inter, ui-sans-serif, system-ui, sans-serif',
  },
  catppuccin: {
    bg: '#1e1e2e', surface: '#24273a', surface2: '#2e3247', text: '#cad3f5',
    muted: '#a5adcb', accent: '#f5a97f', accent2: '#eed49f', border: '#363a4f',
    radius: '18px', shadow: '0 18px 60px rgba(0, 0, 0, 0.35)', tileSize: '148px',
    font: 'Inter, ui-sans-serif, system-ui, sans-serif',
  },
};

const state = {
  boot: null,
  config: null,
  board: null,
  status: {},
  auth: { authenticated: false, setupRequired: false, csrf: null },
  editing: false,
  search: '',
  searchResults: [],
  searchIndex: 0,
  weather: null,
  widgets: [],
  widgetsTimer: null,
  iconList: null,
  iconTarget: null,
  settings: null,
  settingsTab: 'theme',
  dragging: null,
  eventSource: null,
  pollTimer: null,
  reconnectTimer: null,
  clockTimer: null,
  weatherTimer: null,
  configError: '',
  discovery: null,
};

function esc(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function safeURL(value) {
  try {
    const parsed = new URL(String(value || ''), window.location.origin);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : '#';
  } catch {
    return '#';
  }
}

function initial(name) {
  const text = String(name || '?').trim();
  return text ? text.slice(0, 1).toUpperCase() : '?';
}

function slugify(value) {
  return String(value || '').toLowerCase().trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '') || 'item';
}

function fmtLatency(ms) {
  if (ms === undefined || ms === null) return '';
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

function statusFor(item) {
  return state.status[item.statusKey] || null;
}

function statusClass(result) {
  if (!result) return '';
  if (!result.up) return 'down';
  if (result.certExpiry) {
    const days = (new Date(result.certExpiry).getTime() - Date.now()) / 86400000;
    if (days < 14) return 'warn';
  }
  return 'up';
}

function statusText(result) {
  if (!result) return 'not checked';
  if (!result.up) {
    if (result.error) return result.error;
    return `down · ${result.statusCode || 'error'}`;
  }
  let text = `up · ${fmtLatency(result.latencyMs)}`;
  if (result.certExpiry) {
    const days = Math.ceil((new Date(result.certExpiry).getTime() - Date.now()) / 86400000);
    if (days < 30) text += ` · cert ${days}d`;
  }
  return text;
}

function weatherEmoji(code, isDay) {
  if (code === 0) return isDay ? '☀️' : '🌙';
  if ([1, 2].includes(code)) return isDay ? '🌤️' : '🌙';
  if (code === 3) return '☁️';
  if ([45, 48].includes(code)) return '🌫️';
  if ([51, 53, 55, 56, 57].includes(code)) return '🌦️';
  if ([61, 63, 65, 66, 67, 80, 81, 82].includes(code)) return '🌧️';
  if ([71, 73, 75, 77, 85, 86].includes(code)) return '❄️';
  if ([95, 96, 99].includes(code)) return '⛈️';
  return '🌡️';
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  let body = options.body;
  if (body && !(body instanceof FormData) && typeof body !== 'string') {
    headers.set('Content-Type', 'application/json');
    body = JSON.stringify(body);
  }
  if (state.auth.csrf && !['GET', 'HEAD'].includes((options.method || 'GET').toUpperCase())) {
    headers.set('X-CSRF-Token', state.auth.csrf);
  }
  const response = await fetch(API(path), {
    credentials: 'same-origin',
    ...options,
    headers,
    body,
  });
  const type = response.headers.get('content-type') || '';
  let payload = null;
  if (type.includes('application/json')) {
    payload = await response.json().catch(() => null);
  } else {
    payload = await response.text().catch(() => '');
  }
  if (!response.ok) {
    const message = payload?.error?.message || payload?.message || response.statusText || `HTTP ${response.status}`;
    throw new Error(message);
  }
  return payload;
}

function toast(message, kind = '') {
  const root = $('#toast-root');
  const element = document.createElement('div');
  element.className = `toast ${kind}`.trim();
  element.textContent = message;
  root.appendChild(element);
  setTimeout(() => {
    element.style.opacity = '0';
    element.style.transform = 'translateY(8px)';
    element.style.transition = 'all 160ms ease';
    setTimeout(() => element.remove(), 180);
  }, 3600);
}

function branding() {
  return {
    name: state.config?.branding?.name || 'Blackflame',
    description: state.config?.branding?.description || 'Homelab dashboard',
    logo: state.config?.branding?.logo || '',
  };
}

function brandingLogoURL() {
  const logo = branding().logo;
  return logo ? `${BASE}icons/${encodeURIComponent(logo)}` : `${BASE}blackflame.svg`;
}

function layoutSettings() {
  return {
    showHero: false,
    showSearch: true,
    showClock: true,
    showWeather: true,
    ...(state.config?.layout || {}),
  };
}

function currentBoard() {
  if (!state.config?.boards?.length) return null;
  return state.config.boards.find((board) => board.slug === state.board?.slug) || state.config.boards[0];
}

function chooseBoard() {
  const boards = state.config?.boards || [];
  let slug = '';
  const path = window.location.pathname;
  const base = BASE.endsWith('/') ? BASE : `${BASE}/`;
  if (path.startsWith(base)) {
    const rest = path.slice(base.length);
    const match = rest.match(/^b\/([^/]+)/);
    if (match) slug = decodeURIComponent(match[1]);
  }
  state.board = boards.find((board) => board.slug === slug) || boards[0] || null;
}

function navigateBoard(slug) {
  const board = state.config?.boards?.find((entry) => entry.slug === slug);
  if (!board) return;
  state.board = board;
  history.pushState({}, '', `${BASE}b/${encodeURIComponent(slug)}`);
  document.title = `${board.name} · ${branding().name}`;
  applyTheme();
  render();
}

function applyTheme() {
  const globalTheme = state.config?.theme || {};
  const boardTheme = currentBoard()?.theme || {};
  const theme = {
    ...globalTheme,
    ...boardTheme,
    variables: { ...(globalTheme.variables || {}), ...(boardTheme.variables || {}) },
  };
  const presetName = boardTheme.preset || globalTheme.preset || 'blackflame';
  const preset = PRESETS[presetName] || PRESETS.blackflame;
  const variables = { ...preset, ...(theme.variables || {}) };
  const root = document.documentElement;
  for (const [key, value] of Object.entries(variables)) {
    root.style.setProperty(`--bf-${key}`, value);
  }
  root.style.colorScheme = presetName === 'light' ? 'light' : 'dark';
  const custom = $('#custom-css');
  if (custom) {
    custom.href = `${BASE}custom.css?v=${Date.now()}`;
  }
}

function render() {
  const app = $('#app');
  if (!state.config) {
    app.innerHTML = `<div class="loading"><div><div class="spinner"></div>Loading Blackflame…</div></div>`;
    return;
  }
  const board = currentBoard();
  state.board = board;
  app.innerHTML = `
    ${renderTopbar()}
    <div class="app-shell">
      ${layoutSettings().showHero ? renderHero(board) : ''}
      ${renderWidgets()}
      ${state.editing ? renderEditBanner(board) : ''}
      ${state.configError ? `<div class="edit-banner"><span><strong>Config error:</strong> ${esc(state.configError)} — serving the last good configuration.</span></div>` : ''}
      ${renderBoard(board)}
      ${renderFooter()}
    </div>
  `;
  attachIconFallbacks();
  updateStatusDots();
  if (state.search) {
    renderSearchResults();
  }
}

function renderTopbar() {
  const boards = state.config?.boards || [];
  const board = currentBoard();
  const editing = state.editing;
  const auth = state.auth;
  const layout = layoutSettings();
  const brand = branding();
  return `
    <header class="topbar">
      <div class="topbar-inner">
        <a class="brand" href="${BASE}" data-action="home" aria-label="${esc(brand.name)} home">
          <span class="brand-mark"><img class="brand-logo" data-brand-logo src="${esc(brandingLogoURL())}" alt=""></span>
          <span>${esc(brand.name)}<small>${esc(brand.description)}</small></span>
        </a>
        ${boards.length > 1 ? `
          <select class="board-switcher" data-board-switcher aria-label="Switch board">
            ${boards.map((entry) => `<option value="${esc(entry.slug)}" ${entry.slug === board?.slug ? 'selected' : ''}>${esc(entry.name)}</option>`).join('')}
          </select>
        ` : ''}
        ${layout.showSearch ? `
          <div class="search">
            <div class="search-box">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle><path d="m20 20-3.5-3.5"></path></svg>
              <input data-search-input type="search" autocomplete="off" spellcheck="false" placeholder="Search apps and bookmarks…" value="${esc(state.search)}" aria-label="Search">
              <kbd>/</kbd>
            </div>
            <div class="search-results hidden" data-search-results></div>
          </div>
        ` : '<span class="topbar-spacer"></span>'}
        ${!layout.showHero ? renderTopbarStatus(layout) : ''}
        <div class="top-actions">
          ${auth.authenticated ? `
            ${state.boot?.readOnly ? `<span class="btn ghost" title="Managed by YAML configuration">Read-only</span>` : `
              <button class="btn ${editing ? 'primary' : ''}" data-action="edit">
                <span>${editing ? 'Done' : 'Edit'}</span>
              </button>
              ${editing ? `<button class="btn icon-only" data-action="settings" title="Settings" aria-label="Settings">⚙</button>` : ''}
            `}
            <button class="btn ghost icon-only" data-action="logout" title="Log out" aria-label="Log out">⏻</button>
          ` : `
            <button class="btn primary" data-action="${auth.setupRequired ? 'setup' : 'login'}">
              <span>${auth.setupRequired ? 'Set up' : 'Login'}</span>
            </button>
          `}
        </div>
      </div>
    </header>
  `;
}

function renderTopbarStatus(layout) {
  if (!layout.showClock && !layout.showWeather) return '';
  const weather = state.weather;
  const showWeather = layout.showWeather && Boolean(weather);
  const unit = weather?.units === 'imperial' ? '°F' : '°C';
  const now = new Date();
  return `
    <div class="top-status" id="top-status">
      ${layout.showClock ? `<span class="top-clock" data-top-clock>${now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>` : ''}
      ${showWeather ? `<span class="top-weather" title="${esc(weather.location || 'Weather')}">${weatherEmoji(weather.weatherCode, weather.isDay)} ${Math.round(weather.temperature)}${unit}</span>` : ''}
    </div>
  `;
}

function renderHero(board) {
  const layout = layoutSettings();
  const sections = board?.sections || [];
  const count = sections.reduce((total, section) => total + (section.items?.length || 0), 0);
  return `
    <section class="hero">
      <div>
        <h1>${esc(board?.name || 'Blackflame')}</h1>
        ${board ? `<p>${count} ${count === 1 ? 'item' : 'items'} across ${sections.length} ${sections.length === 1 ? 'section' : 'sections'}</p>` : ''}
      </div>
      <div class="hero-side" id="hero-side">
        ${layout.showClock ? renderClockCard() : ''}
        ${layout.showWeather && state.weather ? renderWeatherCard() : ''}
      </div>
    </section>
  `;
}

function renderClockCard() {
  const now = new Date();
  return `
    <div class="clock-card">
      <div class="time">${now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</div>
      <div class="date">${now.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })}</div>
    </div>
  `;
}

function renderWeatherCard() {
  const weather = state.weather;
  if (!weather) return '';
  const unit = weather.units === 'imperial' ? '°F' : '°C';
  return `
    <div class="weather-card" title="${esc(weather.location || 'Weather')}">
      <div class="weather-icon">${weatherEmoji(weather.weatherCode, weather.isDay)}</div>
      <div>
        <div class="weather-temp">${Math.round(weather.temperature)}${unit}</div>
        <div class="weather-place">${esc(weather.location || 'Local weather')} · ${Math.round(weather.high)}${unit} / ${Math.round(weather.low)}${unit}</div>
        <div class="weather-meta">${weather.humidity}% humidity · ${Math.round(weather.windSpeed)} ${weather.units === 'imperial' ? 'mph' : 'km/h'}</div>
      </div>
    </div>
  `;
}

function renderEditBanner(board) {
  return `
    <div class="edit-banner">
      <span><strong>Editing ${esc(board?.name || 'board')}.</strong> Drag tiles to reorder. Changes save immediately.</span>
      <span style="display:flex;gap:7px;flex-wrap:wrap">
        <button class="btn small" data-action="edit-board">Edit board</button>
        <button class="btn small" data-action="add-board">+ Board</button>
        <button class="btn small" data-action="add-section">+ Section</button>
        <button class="btn small" data-action="settings">Settings</button>
      </span>
    </div>
  `;
}

function renderBoard(board) {
  if (!board) {
    return `<div class="empty">No boards yet. ${state.editing ? 'Create one to get started.' : 'Log in to create your first board.'}</div>`;
  }
  const pinned = [];
  for (const section of board.sections || []) {
    for (const item of section.items || []) {
      if (item.pinned && !item.hidden) pinned.push({ item, section });
    }
  }
  return `
    <main class="board">
      ${pinned.length ? `<section class="section pinned-section" aria-labelledby="pinned-heading"><div class="section-head"><h2 id="pinned-heading">Pinned</h2><span class="line"></span></div><div class="pinned-row">${pinned.map(({ item, section }) => renderTile(item, section, board)).join('')}</div></section>` : ''}
      ${(board.sections || []).map((section) => renderSection(section, board)).join('')}
      ${state.editing ? `<button class="btn ghost" data-action="add-section">+ Add section</button>` : ''}
    </main>
  `;
}

function renderSection(section, board) {
  const items = section.items || [];
  return `
    <section class="section" data-section-id="${section.id || ''}">
      <div class="section-head">
        <h2>${esc(section.name)}${section.pinned ? ' · pinned' : ''}${section.public === false ? ' · private' : ''}</h2>
        <span class="line"></span>
        ${state.editing ? `
          <div class="section-actions">
            <button class="btn small" data-action="add-item" data-section-id="${section.id || ''}" data-board-id="${board.id || ''}">+ Item</button>
            <button class="btn small icon-only" data-action="move-section-up" data-section-id="${section.id || ''}" data-board-id="${board.id || ''}" title="Move section up">↑</button>
            <button class="btn small icon-only" data-action="move-section-down" data-section-id="${section.id || ''}" data-board-id="${board.id || ''}" title="Move section down">↓</button>
            <button class="btn small icon-only" data-action="edit-section" data-section-id="${section.id || ''}" data-board-id="${board.id || ''}" title="Edit section">✎</button>
            <button class="btn small icon-only danger" data-action="delete-section" data-section-id="${section.id || ''}" title="Delete section">×</button>
          </div>
        ` : ''}
      </div>
      ${items.length ? `<div class="tile-grid" data-drop-section="${section.id || ''}">${items.map((item) => renderTile(item, section, board)).join('')}</div>` : `<div class="empty">This section is empty.${state.editing ? ' Add an item above.' : ''}</div>`}
    </section>
  `;
}

function renderTile(item, section, board) {
  const result = statusFor(item);
  const dot = statusClass(result);
  const description = item.description || item.group || (item.kind === 'bookmark' ? 'Bookmark' : '');
  const icon = item.icon || '';
  const showEdit = state.editing;
  return `
    <article class="tile" data-item-id="${item.id || ''}" data-section-id="${section.id || ''}" data-status-key="${esc(item.statusKey || '')}" draggable="${showEdit ? 'true' : 'false'}">
      <a class="tile-link" href="${esc(safeURL(item.url))}" target="_blank" rel="noopener noreferrer">
        <div class="tile-icon-wrap">
          ${icon ? `<img class="tile-icon" data-icon src="${esc(`${BASE}icons/${encodeURIComponent(icon)}`)}" alt="" loading="lazy">` : ''}
          <span class="tile-fallback ${icon ? 'hidden' : ''}" data-fallback>${esc(initial(item.name))}</span>
        </div>
        <div class="tile-body">
          <div class="tile-name">${esc(item.name)}</div>
          ${description ? `<div class="tile-desc">${esc(description)}</div>` : ''}
          <div class="tile-meta">
            <span class="status-dot ${dot}" data-status-dot data-status-key="${esc(item.statusKey || '')}" title="${esc(statusText(result))}"></span>
            <span class="tile-status" data-status-text>${esc(statusText(result))}</span>
            ${item.hidden ? '<span>· hidden</span>' : ''}
            ${item.public === false ? '<span>· private</span>' : ''}
          </div>
        </div>
        <span class="tile-launch" aria-hidden="true">↗</span>
      </a>
      ${showEdit ? `
        <div class="tile-edit">
          <button class="btn small icon-only" data-action="move-item-up" data-item-id="${item.id || ''}" data-section-id="${section.id || ''}" title="Move item up">↑</button>
          <button class="btn small icon-only" data-action="move-item-down" data-item-id="${item.id || ''}" data-section-id="${section.id || ''}" title="Move item down">↓</button>
          <button class="btn small icon-only" data-action="edit-item" data-item-id="${item.id || ''}" data-section-id="${section.id || ''}" data-board-id="${board.id || ''}" title="Edit item">✎</button>
          <button class="btn small icon-only danger" data-action="delete-item" data-item-id="${item.id || ''}" title="Delete item">×</button>
        </div>
      ` : ''}
    </article>
  `;
}

function renderFooter() {
  const version = state.boot?.version?.version || 'dev';
  return `
    <footer class="footer">
      <span>${esc(branding().name)} <code>${esc(version)}</code> · ${esc(state.boot?.mode || 'ui')} mode${state.boot?.readOnly ? ' · read-only' : ''}</span>
    </footer>
  `;
}

function renderWidgets() {
  return `<div id="widgets" class="widget-grid ${state.widgets.length ? '' : 'hidden'}">${state.widgets.map(renderWidgetCard).join('')}</div>`;
}

function updateWidgets() {
  const container = $('#widgets');
  if (!container) return;
  container.innerHTML = state.widgets.map(renderWidgetCard).join('');
  container.classList.toggle('hidden', state.widgets.length === 0);
}

function renderWidgetCard(card) {
  return `
    <article class="widget-card widget-${esc(card.type)}">
      <div class="widget-head">
        <span class="widget-title">${esc(card.title || card.type)}</span>
        <span class="widget-type">${esc(card.type)}</span>
      </div>
      ${card.error ? `<div class="widget-error">${esc(card.error)}</div>` : renderWidgetBody(card)}
    </article>
  `;
}

function renderWidgetBody(card) {
  const data = card.data || {};
  if (card.type === 'pihole') {
    return `
      <div class="widget-main">${formatNumber(data.queries)}</div>
      <div class="widget-sub">queries today · ${formatNumber(data.blocked)} blocked</div>
      <div class="widget-meter"><span style="width:${Math.min(100, Math.max(0, data.percentBlocked || 0))}%"></span></div>
      <div class="widget-meta">${Number(data.percentBlocked || 0).toFixed(1)}% blocked · ${formatNumber(data.domainsBlocked)} domains</div>
    `;
  }
  if (card.type === 'proxmox') {
    return `
      <div class="widget-main">${data.nodesOnline || 0}/${data.nodes || 0} <small>nodes online</small></div>
      <div class="widget-sub">CPU ${Number(data.cpuPercent || 0).toFixed(0)}% · RAM ${formatBytes(data.memoryUsed)} / ${formatBytes(data.memoryTotal)}</div>
      <div class="widget-meta">${data.vmsRunning || 0}/${data.vmsTotal || 0} VMs running</div>
    `;
  }
  if (card.type === 'kubernetes') {
    return `
      <div class="widget-main">${data.nodesReady || 0}/${data.nodesTotal || 0} <small>nodes ready</small></div>
      <div class="widget-sub">${data.podsRunning || 0} pods running · ${data.podsFailing || 0} failing</div>
      <div class="widget-meta">${data.podsTotal || 0} pods total</div>
    `;
  }
  if (card.type === 'prometheus') {
    const values = data.queries || [];
    return values.map((query) => `
      <div class="widget-row"><span>${esc(query.label)}</span><strong>${formatWidgetValue(query.value, query.unit)}</strong></div>
    `).join('') || '<div class="widget-meta">No query results</div>';
  }
  if (card.type === 'calendar') {
    const events = data.events || [];
    return events.map((event) => `
      <div class="widget-row widget-event">
        <span><strong>${esc(event.summary)}</strong><small>${esc(event.location || '')}</small></span>
        <span>${formatEventTime(event.start)}</span>
      </div>
    `).join('') || '<div class="widget-meta">No upcoming events</div>';
  }
  return `<pre class="widget-json">${esc(JSON.stringify(data, null, 2))}</pre>`;
}

function formatNumber(value) {
  const number = Number(value || 0);
  if (!Number.isFinite(number)) return '0';
  if (Math.abs(number) >= 1_000_000) return `${(number / 1_000_000).toFixed(1)}M`;
  if (Math.abs(number) >= 1_000) return `${(number / 1_000).toFixed(1)}k`;
  return Number.isInteger(number) ? String(number) : number.toFixed(1);
}

function formatWidgetValue(value, unit) {
  if (unit === 'bytes') return formatBytes(value);
  return `${formatNumber(value)}${esc(unit || '')}`;
}

function formatBytes(value) {
  let bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let index = 0;
  while (bytes >= 1024 && index < units.length - 1) {
    bytes /= 1024;
    index++;
  }
  return `${bytes.toFixed(bytes >= 10 ? 0 : 1)} ${units[index]}`;
}

function formatEventTime(value) {
  try {
    return new Date(value).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}

function attachIconFallbacks() {
  for (const image of $$('img[data-brand-logo]')) {
    image.addEventListener('error', () => {
      if (!image.src.endsWith('/blackflame.svg')) {
        image.src = `${BASE}blackflame.svg`;
      }
    }, { once: true });
  }
  for (const image of $$('img[data-icon]')) {
    image.addEventListener('error', () => {
      image.classList.add('hidden');
      const fallback = image.parentElement?.querySelector('[data-fallback]');
      if (fallback) fallback.classList.remove('hidden');
    }, { once: true });
  }
}

function updateStatusDots() {
  for (const dot of $$('[data-status-dot]')) {
    const key = dot.getAttribute('data-status-key');
    const result = state.status[key];
    dot.className = `status-dot ${statusClass(result)}`;
    dot.title = statusText(result);
    const text = dot.parentElement?.querySelector('[data-status-text]');
    if (text) text.textContent = statusText(result);
  }
}

function updateHeroSide() {
  const layout = layoutSettings();
  const side = $('#hero-side');
  if (side) {
    side.innerHTML = `${layout.showClock ? renderClockCard() : ''}${layout.showWeather && state.weather ? renderWeatherCard() : ''}`;
  }
  updateTopbarStatus();
}

function updateTopbarStatus() {
  const current = $('#top-status');
  const layout = layoutSettings();
  const html = renderTopbarStatus(layout);
  if (!html) {
    current?.remove();
    return;
  }
  if (!current) return;
  const wrapper = document.createElement('div');
  wrapper.innerHTML = html;
  const next = wrapper.firstElementChild;
  if (next) current.replaceWith(next);
}

function searchIndex() {
  const board = currentBoard();
  if (!board) return [];
  const entries = [];
  for (const section of board.sections || []) {
    for (const item of section.items || []) {
      if (item.hidden) continue;
      entries.push({ item, section });
    }
  }
  return entries;
}

function computeSearch(query) {
  state.search = query;
  state.searchIndex = 0;
  const trimmed = query.trim();
  if (!trimmed) {
    state.searchResults = [];
    return;
  }
  const provider = (state.config?.search?.providers || []).find((entry) => {
    const prefix = String(entry.prefix || '');
    return trimmed.toLowerCase().startsWith(`${prefix.toLowerCase()} `);
  });
  if (provider) {
    state.searchResults = [{ type: 'provider', provider, query: trimmed.slice(provider.prefix.length).trim() }];
    return;
  }
  const needle = trimmed.toLowerCase();
  const matches = searchIndex()
    .filter(({ item }) => {
      const haystack = `${item.name || ''} ${item.description || ''} ${item.url || ''} ${item.group || ''}`.toLowerCase();
      return haystack.includes(needle);
    })
    .sort((a, b) => {
      if (a.item.pinned !== b.item.pinned) return a.item.pinned ? -1 : 1;
      return String(a.item.name).localeCompare(String(b.item.name));
    })
    .slice(0, 12);
  state.searchResults = matches.map(({ item, section }) => ({ type: 'item', item, section }));
}

function renderSearchResults() {
  const container = $('[data-search-results]');
  if (!container) return;
  if (!state.search.trim()) {
    container.classList.add('hidden');
    container.innerHTML = '';
    return;
  }
  const results = state.searchResults;
  container.classList.remove('hidden');
  if (!results.length) {
    container.innerHTML = `<div class="search-result"><span>No local matches. Try a provider prefix like <strong>/g</strong>.</span></div>`;
    return;
  }
  container.innerHTML = results.map((result, index) => {
    if (result.type === 'provider') {
      return `<div class="search-result ${index === state.searchIndex ? 'active' : ''}" data-search-result="${index}">
        <span class="mini-icon">${esc(result.provider.name.slice(0, 1))}</span>
        <span><strong>Search ${esc(result.provider.name)}</strong><span>${esc(result.query)}</span></span>
      </div>`;
    }
    const item = result.item;
    const icon = item.icon ? `<img src="${esc(`${BASE}icons/${encodeURIComponent(item.icon)}`)}" alt="">` : esc(initial(item.name));
    return `<div class="search-result ${index === state.searchIndex ? 'active' : ''}" data-search-result="${index}">
      <span class="mini-icon">${icon}</span>
      <span><strong>${esc(item.name)}</strong><span>${esc(item.description || item.url || '')}</span></span>
    </div>`;
  }).join('');
}

function openSearchResult(index) {
  const result = state.searchResults[index];
  if (!result) return;
  if (result.type === 'provider') {
    const url = String(result.provider.url || '').replace('%s', encodeURIComponent(result.query));
    if (safeURL(url) !== '#') window.open(url, '_blank', 'noopener,noreferrer');
  } else {
    window.open(safeURL(result.item.url), '_blank', 'noopener,noreferrer');
  }
  state.search = '';
  state.searchResults = [];
  const input = $('[data-search-input]');
  if (input) input.value = '';
  renderSearchResults();
}

function pinnedItems() {
  const board = currentBoard();
  const items = [];
  for (const section of board?.sections || []) {
    for (const item of section.items || []) {
      if (item.pinned && !item.hidden) items.push(item);
    }
  }
  return items;
}

function openModal(html, wide = false, replace = false) {
  const root = $('#modal-root');
  if (replace) {
    root.querySelectorAll('.modal-backdrop').forEach((element) => element.remove());
  }
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `<div class="modal ${wide ? 'wide' : ''}" role="dialog" aria-modal="true">${html}</div>`;
  root.appendChild(backdrop);
  backdrop.addEventListener('click', (event) => {
    if (event.target === backdrop) closeModal();
  });
}

function closeModal() {
  const root = $('#modal-root');
  const backdrops = root.querySelectorAll('.modal-backdrop');
  if (backdrops.length) backdrops[backdrops.length - 1].remove();
}

function modalHeader(title, subtitle = '') {
  return `<div class="modal-header"><div><h2>${esc(title)}</h2>${subtitle ? `<div class="hint" style="color:var(--bf-muted);font-size:.76rem;margin-top:3px">${esc(subtitle)}</div>` : ''}</div><button class="btn icon-only ghost" data-action="close-modal" aria-label="Close">×</button></div>`;
}

function openLoginModal(setup = false) {
  openModal(`
    ${modalHeader(setup ? `Set up ${branding().name}` : `Unlock ${branding().name}`, setup ? 'Choose a password for this instance. There is no default password.' : 'Enter the edit-mode password.')}
    <form data-form="${setup ? 'setup' : 'login'}">
      <div class="modal-body">
        <div class="field">
          <label for="password">${setup ? 'New password' : 'Password'}</label>
          <input id="password" name="password" type="password" autocomplete="${setup ? 'new-password' : 'current-password'}" required minlength="${setup ? '8' : '1'}" autofocus>
          ${setup ? '<div class="hint">At least 8 characters. Stored as an Argon2id hash.</div>' : ''}
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn ghost" data-action="close-modal">Cancel</button>
        <button type="submit" class="btn primary">${setup ? 'Set password' : 'Unlock'}</button>
      </div>
    </form>
  `);
}

function sectionOptions(selectedId) {
  const board = currentBoard();
  return (board?.sections || []).map((section) => `<option value="${section.id}" ${section.id === selectedId ? 'selected' : ''}>${esc(section.name)}</option>`).join('');
}

function openItemModal(item = null, sectionId = null, boardId = null) {
  const board = currentBoard();
  const targetSection = item ? (board?.sections || []).find((section) => (section.items || []).some((entry) => entry.id === item.id)) : null;
  const selectedSection = sectionId || targetSection?.id || board?.sections?.[0]?.id || '';
  const isEdit = Boolean(item);
  openModal(`
    ${modalHeader(isEdit ? 'Edit item' : 'Add item')}
    <form data-form="item">
      <div class="modal-body">
        <div class="form-grid">
          <input type="hidden" name="id" value="${item?.id || ''}">
          <input type="hidden" name="boardId" value="${boardId || board?.id || ''}">
          <div class="field"><label>Section</label><select name="sectionId">${sectionOptions(selectedSection)}</select></div>
          <div class="field"><label>Type</label><select name="kind"><option value="app" ${item?.kind !== 'bookmark' ? 'selected' : ''}>App</option><option value="bookmark" ${item?.kind === 'bookmark' ? 'selected' : ''}>Bookmark</option></select></div>
          <div class="field"><label>Name</label><input name="name" value="${esc(item?.name || '')}" required></div>
          <div class="field"><label>URL</label><input name="url" type="url" placeholder="https://…" value="${esc(item?.url || '')}" required></div>
          <div class="field full"><label>Icon</label><div style="display:flex;gap:8px"><input id="item-icon" name="icon" value="${esc(item?.icon || '')}" placeholder="immich, pi-hole, or custom-…"><button type="button" class="btn" data-action="icon-picker" data-target="item-icon">Browse</button></div><div class="hint">Use a dashboard-icons slug or upload a custom icon.</div></div>
          <div class="field full"><label>Description</label><input name="description" value="${esc(item?.description || '')}" placeholder="Optional note"></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="public" ${item?.public !== false ? 'checked' : ''}> Public</label></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="pinned" ${item?.pinned ? 'checked' : ''}> Pinned</label></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="hidden" ${item?.hidden ? 'checked' : ''}> Hidden</label></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="checkEnabled" ${item?.checkEnabled !== false ? 'checked' : ''}> Status checks</label></div>
        </div>
      </div>
      <div class="modal-footer"><button type="button" class="btn ghost" data-action="close-modal">Cancel</button><button type="submit" class="btn primary">${isEdit ? 'Save item' : 'Add item'}</button></div>
    </form>
  `);
}

function openSectionModal(section = null, boardId = null) {
  const board = currentBoard();
  openModal(`
    ${modalHeader(section ? 'Edit section' : 'Add section')}
    <form data-form="section">
      <div class="modal-body">
        <div class="form-grid">
          <input type="hidden" name="id" value="${section?.id || ''}">
          <input type="hidden" name="boardId" value="${boardId || board?.id || ''}">
          <div class="field full"><label>Name</label><input name="name" value="${esc(section?.name || '')}" required autofocus></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="public" ${section?.public !== false ? 'checked' : ''}> Public</label></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="pinned" ${section?.pinned ? 'checked' : ''}> Pinned</label></div>
        </div>
      </div>
      <div class="modal-footer"><button type="button" class="btn ghost" data-action="close-modal">Cancel</button><button type="submit" class="btn primary">${section ? 'Save section' : 'Add section'}</button></div>
    </form>
  `);
}

function openBoardModal(board = null) {
  openModal(`
    ${modalHeader(board ? 'Edit board' : 'Add board')}
    <form data-form="board">
      <div class="modal-body">
        <div class="form-grid">
          <input type="hidden" name="id" value="${board?.id || ''}">
          <div class="field"><label>Name</label><input name="name" value="${esc(board?.name || '')}" required autofocus></div>
          <div class="field"><label>Slug</label><input name="slug" value="${esc(board?.slug || '')}" placeholder="home" required></div>
          <div class="field"><label class="checkbox"><input type="checkbox" name="public" ${board?.public !== false ? 'checked' : ''}> Public</label></div>
        </div>
      </div>
      <div class="modal-footer">
        ${board ? `<button type="button" class="btn danger" data-action="delete-board" data-board-id="${board.id}">Delete board</button>` : ''}
        <button type="button" class="btn ghost" data-action="close-modal">Cancel</button>
        <button type="submit" class="btn primary">${board ? 'Save board' : 'Add board'}</button>
      </div>
    </form>
  `);
}

async function openSettingsModal(tab = 'theme') {
  try {
    const response = await api('api/v1/admin/settings');
    state.settings = response.settings;
    state.settingsTab = tab;
    renderSettingsModal();
  } catch (error) {
    toast(error.message, 'error');
  }
}

function renderSettingsModal() {
  const tabs = [
    ['branding', 'Branding'], ['layout', 'Layout'], ['theme', 'Theme'], ['search', 'Search'], ['status', 'Status'],
    ['weather', 'Weather'], ['discovery', 'Discovery'], ['integrations', 'Integrations'], ['data', 'Import / Export'], ['about', 'About'],
  ];
  openModal(`
    ${modalHeader('Settings', 'Changes apply immediately after saving.')}
    <div class="modal-body">
      <div class="tabs">${tabs.map(([id, label]) => `<button type="button" class="tab ${state.settingsTab === id ? 'active' : ''}" data-action="settings-tab" data-tab="${id}">${label}</button>`).join('')}</div>
      ${renderSettingsBody()}
    </div>
    <div class="modal-footer">
      ${['branding', 'layout', 'theme', 'search', 'status', 'weather', 'discovery', 'integrations'].includes(state.settingsTab) ? `<button type="button" class="btn primary" data-action="settings-save">Save changes</button>` : ''}
      <button type="button" class="btn ghost" data-action="close-modal">Close</button>
    </div>
  `, true, true);
}

function renderSettingsBody() {
  const settings = state.settings || {};
  switch (state.settingsTab) {
    case 'branding': return renderBrandingSettings(settings.branding || {});
    case 'layout': return renderLayoutSettings(settings.layout || {});
    case 'integrations': return renderIntegrationsSettings(settings.widgets || {});
    case 'search': return renderSearchSettings(settings.search || { providers: [] });
    case 'status': return renderStatusSettings(settings.status || {});
    case 'weather': return renderWeatherSettings(settings.weather || {});
    case 'discovery': return renderDiscoverySettings(settings.discovery || {});
    case 'data': return renderDataSettings();
    case 'about': return renderAboutSettings();
    default: return renderThemeSettings(settings.theme || {});
  }
}

function renderBrandingSettings(branding) {
  return `
    <div class="form-grid">
      <div class="field"><label>Dashboard name</label><input data-branding-name value="${esc(branding.name || 'Blackflame')}" maxlength="80"></div>
      <div class="field"><label>Dashboard description</label><input data-branding-description value="${esc(branding.description || 'Homelab dashboard')}" maxlength="200"></div>
      <div class="field full"><label>Logo</label><div style="display:flex;gap:8px"><input id="branding-logo" data-branding-logo value="${esc(branding.logo || '')}" placeholder="Default logo"><button type="button" class="btn" data-action="icon-picker" data-target="branding-logo">Browse</button><button type="button" class="btn ghost" data-action="clear-branding-logo">Use default</button></div><div class="hint">Choose a dashboard-icons slug or upload a custom logo. Leave empty for the bundled Blackflame logo.</div></div>
    </div>
  `;
}

function renderLayoutSettings(layout) {
  return `
    <div class="form-grid">
      <div class="field full"><label class="checkbox"><input type="checkbox" data-layout-hero ${layout.showHero ? 'checked' : ''}> Show the large board header</label><div class="hint">The top bar keeps the board switcher and compact clock/weather.</div></div>
      <div class="field"><label class="checkbox"><input type="checkbox" data-layout-search ${layout.showSearch !== false ? 'checked' : ''}> Show search bar</label></div>
      <div class="field"><label class="checkbox"><input type="checkbox" data-layout-clock ${layout.showClock !== false ? 'checked' : ''}> Show clock</label></div>
      <div class="field"><label class="checkbox"><input type="checkbox" data-layout-weather ${layout.showWeather !== false ? 'checked' : ''}> Show weather</label></div>
    </div>
    <div class="hint" style="margin-top:12px">When the large header is hidden, the clock and weather move into the top bar.</div>
  `;
}

function renderThemeSettings(theme) {
  const variables = { ...(PRESETS[theme.preset] || PRESETS.blackflame), ...(theme.variables || {}) };
  const fields = ['bg', 'surface', 'surface2', 'text', 'muted', 'accent', 'accent2', 'border'];
  return `
    <div class="form-grid">
      <div class="field"><label>Preset</label><select data-theme-preset>${Object.keys(PRESETS).map((name) => `<option value="${name}" ${theme.preset === name ? 'selected' : ''}>${name}</option>`).join('')}</select></div>
      <div class="field"><label>Radius</label><input data-theme-text="radius" value="${esc(variables.radius || '18px')}"></div>
      ${fields.map((key) => `<div class="field"><label>${key}</label><div class="color-row"><input type="text" data-theme-text="${key}" value="${esc(variables[key] || '')}"><input type="color" data-theme-var="${key}" value="${esc(variables[key] || '#000000')}"></div></div>`).join('')}
      <div class="field full"><label>Custom CSS</label><textarea data-custom-css placeholder="/* Extra CSS served from /custom.css */">${esc(theme.customCss || '')}</textarea><div class="hint">Custom CSS is served as a separate stylesheet. Only administrators can edit it.</div></div>
    </div>
  `;
}

function renderSearchSettings(search) {
  const providers = search.providers || [];
  return `
    <div data-provider-list>
      ${providers.map((provider) => providerRow(provider)).join('')}
    </div>
    <button type="button" class="btn small" data-action="add-provider">+ Add provider</button>
    <div class="hint" style="margin-top:12px">Prefixes open the provider in a new tab when followed by a space, for example <code>/g blackflame</code>.</div>
  `;
}

function providerRow(provider = {}) {
  return `
    <div class="provider-row" data-provider-row>
      <div class="field"><label>Name</label><input data-provider-name value="${esc(provider.name || '')}"></div>
      <div class="field"><label>Prefix</label><input data-provider-prefix value="${esc(provider.prefix || '/')}"></div>
      <div class="field provider-url"><label>URL (%s)</label><input data-provider-url value="${esc(provider.url || '')}"></div>
      <button type="button" class="btn small danger" data-action="remove-provider">Remove</button>
    </div>
  `;
}

function renderStatusSettings(status) {
  return `
    <div class="form-grid">
      <div class="field"><label class="checkbox"><input type="checkbox" data-status-enabled ${status.enabled !== false ? 'checked' : ''}> Enable status checks</label></div>
      <div class="field"><label>Interval (seconds)</label><input type="number" min="5" data-status-interval value="${status.intervalSeconds || 60}"></div>
      <div class="field"><label>Timeout (seconds)</label><input type="number" min="1" data-status-timeout value="${status.timeoutSeconds || 5}"></div>
      <div class="field"><label>Max concurrency</label><input type="number" min="1" max="128" data-status-concurrency value="${status.maxConcurrency || 16}"></div>
      <div class="field"><label>Expected status</label><input data-status-expected value="${esc(status.expectedStatus || '200-399')}" placeholder="200-399"></div>
    </div>
  `;
}

function widgetBaseFields(prefix, base, insecure = true) {
  return `
    <div class="field"><label class="checkbox"><input type="checkbox" data-widget-${prefix}-enabled ${base.enabled ? 'checked' : ''}> Enabled</label></div>
    <div class="field"><label class="checkbox"><input type="checkbox" data-widget-${prefix}-public ${base.public ? 'checked' : ''}> Show publicly</label></div>
    <div class="field"><label>Title</label><input data-widget-${prefix}-title value="${esc(base.title || '')}" placeholder="Optional title"></div>
    <div class="field"><label>Refresh seconds</label><input type="number" min="10" data-widget-${prefix}-refresh value="${base.refreshSeconds || 60}"></div>
    ${insecure ? `<div class="field"><label class="checkbox"><input type="checkbox" data-widget-${prefix}-insecure ${base.insecureSkipVerify ? 'checked' : ''}> Skip TLS verification</label></div>` : ''}
  `;
}

function secretFields(prefix, label, ref = {}) {
  return `
    <div class="field"><label>${esc(label)} env var</label><input data-${prefix}-env value="${esc(ref.env || '')}" placeholder="ENV_VAR_NAME"></div>
    <div class="field"><label>${esc(label)} file</label><input data-${prefix}-file value="${esc(ref.file || '')}" placeholder="/run/secrets/name"></div>
  `;
}

function renderIntegrationsSettings(widgets) {
  const pihole = widgets.pihole || {};
  const proxmox = widgets.proxmox || {};
  const kubernetes = widgets.kubernetes || {};
  const prometheus = widgets.prometheus || {};
  const calendar = widgets.calendar || {};
  const queryText = (prometheus.queries || []).map((query) => `${query.label}|${query.query}|${query.unit || ''}`).join('\n');
  return `
    <div class="hint" style="margin-bottom:16px">All integrations are optional and disabled by default. Secrets are read from environment variables or files; they are never returned to the browser.</div>

    <h3>Pi-hole</h3>
    <div class="form-grid">
      ${widgetBaseFields('pihole', pihole)}
      <div class="field"><label>URL</label><input data-pihole-url value="${esc(pihole.url || '')}" placeholder="https://pihole.example.com"></div>
      ${secretFields('pihole-secret', 'Password / API token', pihole.secret || {})}
    </div>

    <h3 style="margin-top:22px">Proxmox</h3>
    <div class="form-grid">
      ${widgetBaseFields('proxmox', proxmox)}
      <div class="field"><label>URL</label><input data-proxmox-url value="${esc(proxmox.url || '')}" placeholder="https://proxmox.example.com:8006"></div>
      <div class="field"><label>Token ID</label><input data-proxmox-token-id value="${esc(proxmox.tokenId || '')}" placeholder="user@pam!blackflame"></div>
      ${secretFields('proxmox-token', 'Token secret', proxmox.tokenSecret || {})}
      <div class="field"><label>Node filter</label><input data-proxmox-node value="${esc(proxmox.node || '')}" placeholder="Optional node name"></div>
    </div>

    <h3 style="margin-top:22px">Kubernetes</h3>
    <div class="form-grid">
      ${widgetBaseFields('kubernetes', kubernetes, false)}
      <div class="field"><label>API URL</label><input data-kubernetes-api value="${esc(kubernetes.api || '')}" placeholder="In-cluster if empty"></div>
      ${secretFields('kubernetes-token', 'Service account token', kubernetes.token || {})}
      <div class="field"><label>CA file</label><input data-kubernetes-ca value="${esc(kubernetes.caFile || '')}" placeholder="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"></div>
      <div class="field"><label>Namespaces</label><input data-kubernetes-namespaces value="${esc((kubernetes.namespaces || []).join(', '))}" placeholder="All namespaces if empty"></div>
    </div>

    <h3 style="margin-top:22px">Prometheus / NAS metrics</h3>
    <div class="form-grid">
      ${widgetBaseFields('prometheus', prometheus)}
      <div class="field"><label>URL</label><input data-prometheus-url value="${esc(prometheus.url || '')}" placeholder="http://prometheus:9090"></div>
      ${secretFields('prometheus-user', 'Basic auth username', prometheus.username || {})}
      ${secretFields('prometheus-pass', 'Basic auth password', prometheus.password || {})}
      <div class="field full"><label>Queries</label><textarea data-prometheus-queries placeholder="Disk used|node_filesystem_avail_bytes{mountpoint=&quot;/&quot;}|bytes">${esc(queryText)}</textarea><div class="hint">One per line: label|PromQL query|unit. Use node_exporter or smartctl_exporter for NAS data.</div></div>
    </div>

    <h3 style="margin-top:22px">Calendar</h3>
    <div class="form-grid">
      ${widgetBaseFields('calendar', calendar)}
      <div class="field full"><label>iCal URL</label><input data-calendar-url value="${esc(calendar.url || '')}" placeholder="https://calendar.example.com/feed.ics"></div>
      <div class="field"><label>Days ahead</label><input type="number" min="1" max="365" data-calendar-days value="${calendar.days || 7}"></div>
      <div class="field"><label>Max events</label><input type="number" min="1" max="50" data-calendar-max value="${calendar.maxEvents || 5}"></div>
    </div>
  `;
}

function renderWeatherSettings(weather) {
  return `
    <div class="form-grid">
      <div class="field"><label class="checkbox"><input type="checkbox" data-weather-enabled ${weather.enabled ? 'checked' : ''}> Enable weather card</label></div>
      <div class="field"><label>Location label</label><input data-weather-location value="${esc(weather.location || '')}" placeholder="Home"></div>
      <div class="field"><label>Latitude</label><input type="number" step="0.0001" data-weather-lat value="${weather.latitude ?? 0}"></div>
      <div class="field"><label>Longitude</label><input type="number" step="0.0001" data-weather-lon value="${weather.longitude ?? 0}"></div>
      <div class="field"><label>Units</label><select data-weather-units><option value="metric" ${weather.units !== 'imperial' ? 'selected' : ''}>Metric (°C, km/h)</option><option value="imperial" ${weather.units === 'imperial' ? 'selected' : ''}>Imperial (°F, mph)</option></select></div>
    </div>
    <div class="hint" style="margin-top:12px">Weather uses Open-Meteo and requires no API key. The card refreshes every 10 minutes.</div>
  `;
}

function renderDiscoverySettings(discovery) {
  const docker = discovery.docker || {};
  const k8s = discovery.kubernetes || {};
  const status = state.discovery || state.boot?.discovery || {};
  const dockerStatus = status.docker || {};
  const k8sStatus = status.kubernetes || {};
  const statusLine = (entry) => {
    if (entry.error) return `error: ${entry.error}`;
    const synced = entry.lastSync && new Date(entry.lastSync).getFullYear() > 2000;
    if (synced) return `last sync ${new Date(entry.lastSync).toLocaleString()} · ${entry.count || 0} items`;
    return 'not synced yet';
  };
  return `
    <h3 style="margin-top:0">Docker</h3>
    <div class="form-grid">
      <div class="field"><label class="checkbox"><input type="checkbox" data-docker-enabled ${docker.enabled ? 'checked' : ''}> Enable Docker discovery</label></div>
      <div class="field"><label>Socket</label><input data-docker-socket value="${esc(docker.socket || '/var/run/docker.sock')}"></div>
      <div class="field"><label>Interval (seconds)</label><input type="number" min="5" data-docker-interval value="${docker.intervalSeconds || 30}"></div>
      <div class="field full"><div class="hint">Docker: ${esc(statusLine(dockerStatus))}</div></div>
    </div>
    <h3 style="margin-top:22px">Kubernetes</h3>
    <div class="form-grid">
      <div class="field"><label class="checkbox"><input type="checkbox" data-k8s-enabled ${k8s.enabled ? 'checked' : ''}> Enable Kubernetes discovery</label></div>
      <div class="field"><label>Interval (seconds)</label><input type="number" min="5" data-k8s-interval value="${k8s.intervalSeconds || 30}"></div>
      <div class="field"><label>Namespaces (comma separated)</label><input data-k8s-namespaces value="${esc((k8s.namespaces || []).join(', '))}" placeholder="default, apps"></div>
      <div class="field"><label>Label selector</label><input data-k8s-label value="${esc(k8s.labelSelector || '')}" placeholder="app.kubernetes.io/part-of=homelab"></div>
      <div class="field"><label>Ingress class</label><input data-k8s-class value="${esc(k8s.ingressClass || '')}" placeholder="traefik"></div>
      <div class="field"><label>API URL</label><input data-k8s-api value="${esc(k8s.api || '')}" placeholder="in-cluster if empty"></div>
      <div class="field"><label>Token file</label><input data-k8s-token value="${esc(k8s.tokenFile || '')}" placeholder="/var/run/secrets/…/token"></div>
      <div class="field"><label>CA file</label><input data-k8s-ca value="${esc(k8s.caFile || '')}" placeholder="/var/run/secrets/…/ca.crt"></div>
      <div class="field full"><div class="hint">Kubernetes: ${esc(statusLine(k8sStatus))}</div></div>
    </div>
    <div style="margin-top:18px"><button type="button" class="btn" data-action="discovery-refresh">Refresh discovery now</button></div>
    <div class="hint" style="margin-top:8px">Discovery is read-only. Docker labels and Kubernetes annotations populate a Discovered board; user edits are preserved.</div>
  `;
}

function renderDataSettings() {
  return `
    <div class="form-grid">
      <div class="field full"><label>Import Flame database</label><input type="file" data-flame-file accept=".sqlite,.db,application/x-sqlite3"></div>
      <div class="field"><label class="checkbox"><input type="checkbox" data-flame-replace> Replace existing boards and content</label></div>
      <div class="field full"><button type="button" class="btn primary" data-action="import-flame">Import db.sqlite</button></div>
      <div class="field full"><label>Export</label><a class="btn" href="${BASE}api/v1/export">Download YAML</a><div class="hint">Export includes all content and settings. Treat it as sensitive.</div></div>
    </div>
  `;
}

function renderAboutSettings() {
  const version = state.boot?.version || {};
  return `
    <div class="form-grid">
      <div class="field"><label>Version</label><input value="${esc(version.version || 'dev')}" readonly></div>
      <div class="field"><label>Commit</label><input value="${esc(version.commit || 'none')}" readonly></div>
      <div class="field"><label>Go</label><input value="${esc(version.goVersion || '')}" readonly></div>
      <div class="field"><label>Mode</label><input value="${esc(state.boot?.mode || 'ui')}${state.boot?.readOnly ? ' (read-only)' : ''}" readonly></div>
    </div>
    <p style="color:var(--bf-muted);font-size:.82rem;margin-top:18px">${esc(branding().name)} is a local-first homelab dashboard. No telemetry, no external asset CDN at page load. Icons are cached locally from dashboard-icons.</p>
  `;
}

async function openIconPicker(targetId) {
  state.iconTarget = targetId;
  try {
    if (!state.iconList) {
      const response = await api('api/v1/icons');
      state.iconList = response.icons || [];
    }
    openModal(`
      ${modalHeader('Choose an icon', 'Search dashboard-icons or use a custom slug.')}
      <div class="modal-body">
        <div class="field"><input data-icon-search placeholder="Search icons…" autofocus></div>
        <div class="icon-grid" data-icon-grid style="margin-top:14px"></div>
        <div class="form-grid" style="margin-top:16px">
          <div class="field"><label>Custom slug</label><input data-custom-icon placeholder="my-service"></div>
          <div class="field"><label>Upload</label><input type="file" data-icon-upload accept="image/png,image/jpeg,image/webp,image/svg+xml"></div>
        </div>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn" data-action="use-custom-icon">Use slug</button>
        <button type="button" class="btn ghost" data-action="close-modal">Close</button>
      </div>
    `, true);
    renderIconGrid('');
  } catch (error) {
    toast(error.message, 'error');
  }
}

function renderIconGrid(filter) {
  const grid = $('[data-icon-grid]');
  if (!grid) return;
  const needle = filter.toLowerCase();
  const icons = (state.iconList || []).filter((name) => name.toLowerCase().includes(needle));
  grid.innerHTML = icons.map((name) => `
    <button type="button" class="icon-option" data-action="pick-icon" data-icon="${esc(name)}">
      <img src="${esc(`${BASE}icons/${encodeURIComponent(name)}`)}" alt="" loading="lazy">
      <span>${esc(name)}</span>
    </button>
  `).join('') || '<div class="empty">No icons match.</div>';
  attachIconFallbacks();
}

function setIconTarget(name) {
  const input = document.getElementById(state.iconTarget);
  if (input) input.value = name;
  closeModal();
}

async function uploadIcon(file) {
  const form = new FormData();
  form.append('icon', file);
  const response = await api('api/v1/icons', { method: 'POST', body: form });
  setIconTarget(response.name);
  toast('Icon uploaded', 'success');
}

function collectWidgetBase(prefix) {
  return {
    enabled: $(`[data-widget-${prefix}-enabled]`)?.checked || false,
    public: $(`[data-widget-${prefix}-public]`)?.checked || false,
    title: $(`[data-widget-${prefix}-title]`)?.value.trim() || '',
    refreshSeconds: Number($(`[data-widget-${prefix}-refresh]`)?.value || 60),
    insecureSkipVerify: $(`[data-widget-${prefix}-insecure]`)?.checked || false,
  };
}

function collectSecret(prefix) {
  return {
    env: $(`[data-${prefix}-env]`)?.value.trim() || '',
    file: $(`[data-${prefix}-file]`)?.value.trim() || '',
  };
}

function collectIntegrationsSettings() {
  const queries = ($('[data-prometheus-queries]')?.value || '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split('|').map((part) => part.trim());
      return { label: parts[0] || '', query: parts[1] || '', unit: parts[2] || '' };
    })
    .filter((query) => query.label && query.query);
  return {
    pihole: {
      ...collectWidgetBase('pihole'),
      url: $('[data-pihole-url]')?.value.trim() || '',
      secret: collectSecret('pihole-secret'),
    },
    proxmox: {
      ...collectWidgetBase('proxmox'),
      url: $('[data-proxmox-url]')?.value.trim() || '',
      tokenId: $('[data-proxmox-token-id]')?.value.trim() || '',
      tokenSecret: collectSecret('proxmox-token'),
      node: $('[data-proxmox-node]')?.value.trim() || '',
    },
    kubernetes: {
      ...collectWidgetBase('kubernetes'),
      api: $('[data-kubernetes-api]')?.value.trim() || '',
      token: collectSecret('kubernetes-token'),
      caFile: $('[data-kubernetes-ca]')?.value.trim() || '',
      namespaces: ($('[data-kubernetes-namespaces]')?.value || '').split(',').map((value) => value.trim()).filter(Boolean),
    },
    prometheus: {
      ...collectWidgetBase('prometheus'),
      url: $('[data-prometheus-url]')?.value.trim() || '',
      username: collectSecret('prometheus-user'),
      password: collectSecret('prometheus-pass'),
      queries,
    },
    calendar: {
      ...collectWidgetBase('calendar'),
      url: $('[data-calendar-url]')?.value.trim() || '',
      days: Number($('[data-calendar-days]')?.value || 7),
      maxEvents: Number($('[data-calendar-max]')?.value || 5),
    },
  };
}

function collectBrandingSettings() {
  return {
    name: $('[data-branding-name]')?.value.trim() || 'Blackflame',
    description: $('[data-branding-description]')?.value.trim() || 'Homelab dashboard',
    logo: $('[data-branding-logo]')?.value.trim() || '',
  };
}

function collectLayoutSettings() {
  return {
    showHero: $('[data-layout-hero]')?.checked || false,
    showSearch: $('[data-layout-search]')?.checked ?? true,
    showClock: $('[data-layout-clock]')?.checked ?? true,
    showWeather: $('[data-layout-weather]')?.checked ?? true,
  };
}

function collectThemeSettings() {
  const variables = {};
  for (const input of $$('[data-theme-var]')) {
    variables[input.dataset.themeVar] = input.value;
  }
  for (const input of $$('[data-theme-text]')) {
    variables[input.dataset.themeText] = input.value;
  }
  return {
    preset: $('[data-theme-preset]')?.value || 'blackflame',
    variables,
    customCss: $('[data-custom-css]')?.value || '',
  };
}

function collectSearchSettings() {
  const providers = $$('[data-provider-row]').map((row) => ({
    name: $('[data-provider-name]', row)?.value.trim() || '',
    prefix: $('[data-provider-prefix]', row)?.value.trim() || '',
    url: $('[data-provider-url]', row)?.value.trim() || '',
  })).filter((provider) => provider.name && provider.prefix && provider.url);
  return { providers };
}

function collectStatusSettings() {
  return {
    enabled: $('[data-status-enabled]')?.checked ?? true,
    intervalSeconds: Number($('[data-status-interval]')?.value || 60),
    timeoutSeconds: Number($('[data-status-timeout]')?.value || 5),
    maxConcurrency: Number($('[data-status-concurrency]')?.value || 16),
    expectedStatus: $('[data-status-expected]')?.value.trim() || '200-399',
  };
}

function collectWeatherSettings() {
  return {
    enabled: $('[data-weather-enabled]')?.checked || false,
    location: $('[data-weather-location]')?.value.trim() || '',
    latitude: Number($('[data-weather-lat]')?.value || 0),
    longitude: Number($('[data-weather-lon]')?.value || 0),
    units: $('[data-weather-units]')?.value || 'metric',
  };
}

function collectDiscoverySettings() {
  return {
    docker: {
      enabled: $('[data-docker-enabled]')?.checked || false,
      socket: $('[data-docker-socket]')?.value.trim() || '/var/run/docker.sock',
      intervalSeconds: Number($('[data-docker-interval]')?.value || 30),
    },
    kubernetes: {
      enabled: $('[data-k8s-enabled]')?.checked || false,
      intervalSeconds: Number($('[data-k8s-interval]')?.value || 30),
      namespaces: ($('[data-k8s-namespaces]')?.value || '').split(',').map((value) => value.trim()).filter(Boolean),
      labelSelector: $('[data-k8s-label]')?.value.trim() || '',
      ingressClass: $('[data-k8s-class]')?.value.trim() || '',
      api: $('[data-k8s-api]')?.value.trim() || '',
      tokenFile: $('[data-k8s-token]')?.value.trim() || '',
      caFile: $('[data-k8s-ca]')?.value.trim() || '',
    },
  };
}

async function saveSettings() {
  const body = {};
  if (state.settingsTab === 'branding') body.branding = collectBrandingSettings();
  if (state.settingsTab === 'layout') body.layout = collectLayoutSettings();
  if (state.settingsTab === 'integrations') body.widgets = collectIntegrationsSettings();
  if (state.settingsTab === 'theme') body.theme = collectThemeSettings();
  if (state.settingsTab === 'search') body.search = collectSearchSettings();
  if (state.settingsTab === 'status') body.status = collectStatusSettings();
  if (state.settingsTab === 'weather') body.weather = collectWeatherSettings();
  if (state.settingsTab === 'discovery') body.discovery = collectDiscoverySettings();
  try {
    await api('api/v1/settings', { method: 'PATCH', body });
    closeModal();
    await load();
    toast('Settings saved', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function importFlame() {
  const file = $('[data-flame-file]')?.files?.[0];
  if (!file) {
    toast('Choose a Flame db.sqlite file first', 'error');
    return;
  }
  const form = new FormData();
  form.append('flame', file);
  form.append('replace', $('[data-flame-replace]')?.checked ? 'true' : 'false');
  try {
    const result = await api('api/v1/import/flame', { method: 'POST', body: form });
    toast(`Imported ${result.apps} apps, ${result.categories} categories, ${result.bookmarks} bookmarks`, 'success');
    closeModal();
    await load();
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function refreshDiscovery() {
  try {
    const status = await api('api/v1/discovery/refresh', { method: 'POST', body: {} });
    state.discovery = status;
    await load();
    if ($('.modal')) renderSettingsModal();
    toast('Discovery refreshed', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function load() {
  try {
    const boot = await api('api/v1/bootstrap');
    state.boot = boot;
    state.config = boot.config;
    state.status = boot.status || {};
    state.discovery = boot.discovery || null;
    state.configError = boot.configError || '';
    state.auth = {
      authenticated: Boolean(boot.authenticated),
      setupRequired: Boolean(boot.setupRequired),
      csrf: boot.csrf || null,
    };
    if (!boot.authenticated) state.editing = false;
    if (boot.editable === false && boot.readOnly) state.editing = false;
    chooseBoard();
    applyTheme();
    render();
    document.title = `${currentBoard()?.name || branding().name} · ${branding().name}`;
    connectEvents();
    loadWeather();
    loadWidgets();
    if (!state.weatherTimer) state.weatherTimer = setInterval(loadWeather, 10 * 60 * 1000);
    if (!state.widgetsTimer) state.widgetsTimer = setInterval(loadWidgets, 60 * 1000);
    startClock();
    if (boot.setupRequired && !$('#modal-root')?.innerHTML) {
      openLoginModal(true);
    }
  } catch (error) {
    $('#app').innerHTML = `<div class="loading"><div><div class="spinner"></div><p>Could not load Blackflame.</p><p style="color:var(--bf-muted)">${esc(error.message)}</p><button class="btn primary" data-action="retry">Retry</button></div></div>`;
  }
}

async function loadWidgets() {
  try {
    const response = await api('api/v1/widgets');
    state.widgets = response.widgets || [];
  } catch {
    state.widgets = [];
  }
  updateWidgets();
}

async function loadWeather() {
  if (!state.config?.weather?.enabled) {
    state.weather = null;
    updateHeroSide();
    return;
  }
  try {
    state.weather = await api('api/v1/weather');
    updateHeroSide();
  } catch {
    state.weather = null;
    updateHeroSide();
  }
}

function connectEvents() {
  if (state.eventSource) state.eventSource.close();
  if (state.reconnectTimer) clearTimeout(state.reconnectTimer);
  const source = new EventSource(API('api/v1/events'));
  state.eventSource = source;
  source.onopen = () => {
    if (state.pollTimer) {
      clearInterval(state.pollTimer);
      state.pollTimer = null;
    }
  };
  source.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data);
      if (payload.type === 'status' && payload.data?.key) {
        state.status[payload.data.key] = payload.data;
        updateStatusDots();
      }
    } catch {
      // Ignore malformed events.
    }
  };
  source.onerror = () => {
    source.close();
    state.eventSource = null;
    startPolling();
    state.reconnectTimer = setTimeout(connectEvents, 10000);
  };
}

function startPolling() {
  if (state.pollTimer) return;
  state.pollTimer = setInterval(async () => {
    try {
      const response = await api('api/v1/status');
      state.status = response.status || {};
      updateStatusDots();
    } catch {
      // The next interval will retry.
    }
  }, 15000);
}

function startClock() {
  if (state.clockTimer) return;
  state.clockTimer = setInterval(() => {
    const now = new Date();
    const time = $('.clock-card .time');
    const date = $('.clock-card .date');
    const topClock = $('[data-top-clock]');
    if (time) time.textContent = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    if (date) date.textContent = now.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
    if (topClock) topClock.textContent = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }, 30000);
}

async function handleLogin(form, setup) {
  const password = new FormData(form).get('password') || '';
  try {
    const response = await api(`api/v1/auth/${setup ? 'setup' : 'login'}`, { method: 'POST', body: { password } });
    state.auth.authenticated = true;
    state.auth.setupRequired = false;
    state.auth.csrf = response.csrf;
    state.editing = true;
    closeModal();
    await load();
    toast(setup ? 'Edit password set' : 'Edit mode unlocked', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function handleLogout() {
  try {
    await api('api/v1/auth/logout', { method: 'POST', body: {} });
  } catch {
    // Ignore network errors during logout.
  }
  state.auth = { authenticated: false, setupRequired: false, csrf: null };
  state.editing = false;
  await load();
}

async function saveItem(form) {
  const fd = new FormData(form);
  const id = fd.get('id');
  const payload = {
    boardId: Number(fd.get('boardId')),
    sectionId: Number(fd.get('sectionId')),
    kind: fd.get('kind'),
    name: String(fd.get('name') || '').trim(),
    url: String(fd.get('url') || '').trim(),
    icon: String(fd.get('icon') || '').trim(),
    description: String(fd.get('description') || '').trim(),
    public: fd.get('public') === 'on',
    pinned: fd.get('pinned') === 'on',
    hidden: fd.get('hidden') === 'on',
    checkEnabled: fd.get('checkEnabled') === 'on',
  };
  try {
    if (id) {
      await api(`api/v1/items/${id}`, { method: 'PATCH', body: payload });
    } else {
      await api('api/v1/items', { method: 'POST', body: payload });
    }
    closeModal();
    await load();
    toast(id ? 'Item saved' : 'Item added', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function saveSection(form) {
  const fd = new FormData(form);
  const id = fd.get('id');
  const payload = {
    boardId: Number(fd.get('boardId')),
    name: String(fd.get('name') || '').trim(),
    public: fd.get('public') === 'on',
    pinned: fd.get('pinned') === 'on',
  };
  try {
    if (id) {
      await api(`api/v1/sections/${id}`, { method: 'PATCH', body: payload });
    } else {
      await api('api/v1/sections', { method: 'POST', body: payload });
    }
    closeModal();
    await load();
    toast(id ? 'Section saved' : 'Section added', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function saveBoard(form) {
  const fd = new FormData(form);
  const id = fd.get('id');
  const payload = {
    slug: String(fd.get('slug') || '').trim(),
    name: String(fd.get('name') || '').trim(),
    public: fd.get('public') === 'on',
  };
  try {
    if (id) {
      await api(`api/v1/boards/${id}`, { method: 'PATCH', body: payload });
    } else {
      await api('api/v1/boards', { method: 'POST', body: payload });
    }
    closeModal();
    await load();
    toast(id ? 'Board saved' : 'Board added', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function deleteItem(id) {
  if (!window.confirm('Delete this item?')) return;
  try {
    await api(`api/v1/items/${id}`, { method: 'DELETE' });
    await load();
    toast('Item deleted', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function deleteSection(id) {
  if (!window.confirm('Delete this section and all its items?')) return;
  try {
    await api(`api/v1/sections/${id}`, { method: 'DELETE' });
    await load();
    toast('Section deleted', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function deleteBoard(id) {
  if (!window.confirm('Delete this board and all its content?')) return;
  try {
    await api(`api/v1/boards/${id}`, { method: 'DELETE' });
    await load();
    toast('Board deleted', 'success');
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function reorderSectionItems(section, direction) {
  const items = [...(section.items || [])];
  const index = items.findIndex((item) => item.id === direction.id);
  if (index < 0) return;
  const target = index + direction.delta;
  if (target < 0 || target >= items.length) return;
  [items[index], items[target]] = [items[target], items[index]];
  try {
    await api('api/v1/reorder/items', { method: 'POST', body: { sectionId: section.id, ids: items.map((item) => item.id) } });
    await load();
  } catch (error) {
    toast(error.message, 'error');
  }
}

async function reorderSections(board, sectionId, delta) {
  const sections = [...(board.sections || [])];
  const index = sections.findIndex((section) => section.id === sectionId);
  if (index < 0) return;
  const target = index + delta;
  if (target < 0 || target >= sections.length) return;
  [sections[index], sections[target]] = [sections[target], sections[index]];
  try {
    await api('api/v1/reorder/sections', { method: 'POST', body: { boardId: board.id, ids: sections.map((section) => section.id) } });
    await load();
  } catch (error) {
    toast(error.message, 'error');
  }
}

document.addEventListener('click', async (event) => {
  const searchResult = event.target.closest('[data-search-result]');
  if (searchResult) {
    openSearchResult(Number(searchResult.dataset.searchResult));
    return;
  }
  const target = event.target.closest('[data-action]');
  if (!target) return;
  const action = target.dataset.action;
  if (action === 'close-modal') { closeModal(); return; }
  if (action === 'home') { event.preventDefault(); navigateBoard(state.config?.boards?.[0]?.slug || 'home'); return; }
  if (action === 'retry') { await load(); return; }
  if (action === 'login') { openLoginModal(false); return; }
  if (action === 'setup') { openLoginModal(true); return; }
  if (action === 'logout') { await handleLogout(); return; }
  if (action === 'edit') {
    if (!state.auth.authenticated) { openLoginModal(false); return; }
    if (state.boot?.readOnly) { toast('This instance is managed by configuration and is read-only', 'error'); return; }
    state.editing = !state.editing;
    render();
    return;
  }
  if (action === 'settings') { await openSettingsModal(); return; }
  if (action === 'add-item') { openItemModal(null, Number(target.dataset.sectionId), Number(target.dataset.boardId)); return; }
  if (action === 'edit-item') {
    const item = findItem(Number(target.dataset.itemId));
    openItemModal(item, Number(target.dataset.sectionId), Number(target.dataset.boardId));
    return;
  }
  if (action === 'delete-item') { await deleteItem(Number(target.dataset.itemId)); return; }
  if (action === 'add-section') { openSectionModal(null, currentBoard()?.id); return; }
  if (action === 'edit-section') {
    const section = (currentBoard()?.sections || []).find((entry) => entry.id === Number(target.dataset.sectionId));
    openSectionModal(section, Number(target.dataset.boardId));
    return;
  }
  if (action === 'delete-section') { await deleteSection(Number(target.dataset.sectionId)); return; }
  if (action === 'add-board') { openBoardModal(); return; }
  if (action === 'edit-board') { openBoardModal(currentBoard()); return; }
  if (action === 'delete-board') { await deleteBoard(Number(target.dataset.boardId)); return; }
  if (action === 'move-item-up' || action === 'move-item-down') {
    const section = (currentBoard()?.sections || []).find((entry) => entry.id === Number(target.dataset.sectionId));
    if (section) await reorderSectionItems(section, { id: Number(target.dataset.itemId), delta: action === 'move-item-up' ? -1 : 1 });
    return;
  }
  if (action === 'move-section-up' || action === 'move-section-down') {
    await reorderSections(currentBoard(), Number(target.dataset.sectionId), action === 'move-section-up' ? -1 : 1);
    return;
  }
  if (action === 'settings-tab') { state.settingsTab = target.dataset.tab; renderSettingsModal(); return; }
  if (action === 'settings-save') { await saveSettings(); return; }
  if (action === 'add-provider') {
    $('[data-provider-list]')?.insertAdjacentHTML('beforeend', providerRow({ prefix: '/', url: 'https://example.com/search?q=%s' }));
    return;
  }
  if (action === 'remove-provider') { target.closest('[data-provider-row]')?.remove(); return; }
  if (action === 'discovery-refresh') { await refreshDiscovery(); return; }
  if (action === 'import-flame') { await importFlame(); return; }
  if (action === 'icon-picker') { await openIconPicker(target.dataset.target); return; }
  if (action === 'pick-icon') { setIconTarget(target.dataset.icon); return; }
  if (action === 'clear-branding-logo') { const input = $('[data-branding-logo]'); if (input) input.value = ''; return; }
  if (action === 'use-custom-icon') {
    const value = $('[data-custom-icon]')?.value.trim();
    if (value) setIconTarget(value);
    return;
  }
});

document.addEventListener('change', async (event) => {
  if (event.target.matches('[data-board-switcher]')) {
    navigateBoard(event.target.value);
  }
  if (event.target.matches('[data-icon-upload]')) {
    const file = event.target.files?.[0];
    if (file) {
      try { await uploadIcon(file); } catch (error) { toast(error.message, 'error'); }
    }
  }
  if (event.target.matches('[data-theme-preset]')) {
    const preset = PRESETS[event.target.value] || PRESETS.blackflame;
    for (const input of $$('[data-theme-var]')) {
      if (preset[input.dataset.themeVar]) input.value = preset[input.dataset.themeVar];
    }
    for (const input of $$('[data-theme-text]')) {
      if (preset[input.dataset.themeText]) input.value = preset[input.dataset.themeText];
    }
  }
});

document.addEventListener('input', (event) => {
  if (event.target.matches('[data-search-input]')) {
    computeSearch(event.target.value);
    renderSearchResults();
  }
  if (event.target.matches('[data-icon-search]')) {
    renderIconGrid(event.target.value);
  }
  if (event.target.matches('[data-theme-var]')) {
    const text = $(`[data-theme-text="${event.target.dataset.themeVar}"]`);
    if (text) text.value = event.target.value;
  }
});

document.addEventListener('submit', async (event) => {
  const form = event.target.closest('form[data-form]');
  if (!form) return;
  event.preventDefault();
  const kind = form.dataset.form;
  if (kind === 'login') { await handleLogin(form, false); return; }
  if (kind === 'setup') { await handleLogin(form, true); return; }
  if (kind === 'item') { await saveItem(form); return; }
  if (kind === 'section') { await saveSection(form); return; }
  if (kind === 'board') { await saveBoard(form); return; }
});

document.addEventListener('keydown', (event) => {
  const tag = event.target.tagName?.toLowerCase();
  const typing = ['input', 'textarea', 'select'].includes(tag);
  if (event.key === 'Escape') {
    closeModal();
    const input = $('[data-search-input]');
    if (input) { input.value = ''; computeSearch(''); renderSearchResults(); }
    return;
  }
  if (!typing && event.key === '/') {
    event.preventDefault();
    $('[data-search-input]')?.focus();
    return;
  }
  if (event.target.matches('[data-search-input]')) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!state.searchResults.length) return;
      state.searchIndex = (state.searchIndex + (event.key === 'ArrowDown' ? 1 : -1) + state.searchResults.length) % state.searchResults.length;
      renderSearchResults();
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      openSearchResult(state.searchIndex);
      return;
    }
  }
  if (!typing && /^[1-9]$/.test(event.key)) {
    const items = pinnedItems();
    const item = items[Number(event.key) - 1];
    if (item) window.open(safeURL(item.url), '_blank', 'noopener,noreferrer');
  }
});

document.addEventListener('dragstart', (event) => {
  const tile = event.target.closest('.tile[draggable="true"]');
  if (!tile) return;
  state.dragging = { itemId: Number(tile.dataset.itemId), sectionId: Number(tile.dataset.sectionId) };
  tile.classList.add('dragging');
  event.dataTransfer.effectAllowed = 'move';
  event.dataTransfer.setData('text/plain', String(tile.dataset.itemId));
});

document.addEventListener('dragend', () => {
  state.dragging = null;
  for (const tile of $$('.tile.dragging')) tile.classList.remove('dragging');
  for (const section of $$('.section.drop-target')) section.classList.remove('drop-target');
});

document.addEventListener('dragover', (event) => {
  const grid = event.target.closest('.tile-grid');
  if (!grid || !state.dragging) return;
  event.preventDefault();
  grid.closest('.section')?.classList.add('drop-target');
});

document.addEventListener('dragleave', (event) => {
  const section = event.target.closest('.section');
  if (section && !section.contains(event.relatedTarget)) section.classList.remove('drop-target');
});

document.addEventListener('drop', async (event) => {
  const grid = event.target.closest('.tile-grid');
  if (!grid || !state.dragging) return;
  event.preventDefault();
  const section = currentBoard()?.sections?.find((entry) => entry.id === Number(grid.dataset.dropSection));
  if (!section) return;
  if (Number(section.id) !== Number(state.dragging.sectionId)) {
    toast('Move items between sections from the edit dialog', 'error');
    return;
  }
  const targetTile = event.target.closest('.tile');
  const items = [...(section.items || [])];
  const from = items.findIndex((item) => item.id === state.dragging.itemId);
  if (from < 0) return;
  const [moved] = items.splice(from, 1);
  let to = targetTile ? items.findIndex((item) => item.id === Number(targetTile.dataset.itemId)) : items.length;
  if (to < 0) to = items.length;
  items.splice(to, 0, moved);
  try {
    await api('api/v1/reorder/items', { method: 'POST', body: { sectionId: section.id, ids: items.map((item) => item.id) } });
    await load();
  } catch (error) {
    toast(error.message, 'error');
  }
});

function findItem(id) {
  for (const board of state.config?.boards || []) {
    for (const section of board.sections || []) {
      const item = (section.items || []).find((entry) => entry.id === id);
      if (item) return item;
    }
  }
  return null;
}

window.addEventListener('popstate', () => {
  chooseBoard();
  applyTheme();
  render();
});

window.addEventListener('beforeunload', () => {
  state.eventSource?.close();
  if (state.pollTimer) clearInterval(state.pollTimer);
  if (state.clockTimer) clearInterval(state.clockTimer);
  if (state.weatherTimer) clearInterval(state.weatherTimer);
  if (state.widgetsTimer) clearInterval(state.widgetsTimer);
  if (state.reconnectTimer) clearTimeout(state.reconnectTimer);
});

load();
