import './settings'
type Config = {
  Bind: string
  Allowed: string
  HTTP: number
  SOCKS: number
  AutoStart: boolean
  StartHidden: boolean
  User: string
  DownloadKbps: number
  UploadKbps: number
  ParentAddr: string
  ParentType: string
}
type Sample = { RX: number; TX: number }
type Totals = { RX: number; TX: number; Since: number }
type Stat = { Label: string; RX: number; TX: number }
type Stats = { Total: Totals; Days: Stat[] | null; Months: Stat[] | null }
type View = {
  State: string
  Detail: string
  Config: Config
  Uptime: number
  RX: number
  TX: number
  Totals: Totals
  Clients: { Address: string; Port: number; Protocol: string }[] | null
  History: Sample[] | null
  Logs: string[] | null
}
const el = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T
const runtimePath = '/wails/runtime.js'
const runtime = import(/* @vite-ignore */ runtimePath)
async function call(method: string, ...args: unknown[]) {
  return (await runtime).Call.ByName(
    'github.com/ashtray01/ashproxy/desktop/engine.Manager.' + method,
    ...args,
  )
}
const states: Record<string, string> = {
  running: 'Работает',
  stopped: 'Остановлен',
  error: 'Ошибка',
  lost: 'Нет связи с ядром',
}
let view: View | undefined,
  busy = false,
  polling = false,
  initialized = false,
  revision = 0
function message(value: unknown) {
  el('error').textContent = String(value || '')
  el('error').hidden = !value
}
function size(n: number) {
  const units = ['Б', 'КиБ', 'МиБ', 'ГиБ', 'ТиБ']
  let i = 0
  while (n >= 1024 && i < 4) {
    n /= 1024
    i++
  }
  return (
    n.toLocaleString('ru', { maximumFractionDigits: i ? 1 : 0 }) +
    ' ' +
    units[i]
  )
}
const period = el<HTMLSelectElement>('period')
let historic: Sample[] = [],
  fetched = 0,
  statsFetched = 0
async function refreshHistory() {
  const selected = Number(period.value)
  if (!selected) return
  try {
    const values = await call('Traffic', selected)
    if (Number(period.value) === selected) {
      historic = values
      fetched = Date.now()
      chart(historic)
    }
  } catch (e) {
    message(e)
  }
}
period.onchange = () => {
  fetched = 0
  historic = []
  if (Number(period.value)) void refreshHistory()
  else chart(view?.History || [])
}
function chart(points: Sample[]) {
  const seriesSize = Number(period.value) ? Math.max(2, points.length) : 60
  const canvas = el<HTMLCanvasElement>('chart'),
    dpr = devicePixelRatio || 1
  const w = canvas.clientWidth,
    h = canvas.clientHeight
  if (!w || !h) return
  canvas.width = w * dpr
  canvas.height = h * dpr
  const g = canvas.getContext('2d')!
  g.scale(dpr, dpr)
  const peak = Math.max(1, ...points.map((p) => Math.max(p.RX, p.TX)))
  g.strokeStyle = getComputedStyle(document.documentElement).getPropertyValue(
    'color',
  )
  g.globalAlpha = 0.14
  for (let i = 1; i <= 3; i++) {
    g.beginPath()
    g.moveTo(0, (h * i) / 4)
    g.lineTo(w, (h * i) / 4)
    g.stroke()
  }
  g.globalAlpha = 1
  for (const [key, color] of [
    ['RX', '#428ae1'],
    ['TX', '#39aa81'],
  ] as const) {
    g.strokeStyle = color
    g.lineWidth = 1.8
    g.beginPath()
    points.forEach((p, i) => {
      const x = (w * (seriesSize - points.length + i)) / (seriesSize - 1),
        y = h - 2 - ((h - 5) * p[key]) / peak
      if (i) g.lineTo(x, y)
      else g.moveTo(x, y)
    })
    g.stroke()
  }
  canvas.onmousemove = (e) => {
    const i =
      Math.round((e.offsetX / w) * (seriesSize - 1)) -
      (seriesSize - points.length)
    if (points[i])
      canvas.title =
        'RX ' + size(points[i].RX) + '/с · TX ' + size(points[i].TX) + '/с'
    else canvas.title = ''
  }
}
function rows() {
  const body = el('rows')
  body.replaceChildren()
  const q = el<HTMLInputElement>('search').value.toLowerCase()
  for (const c of view?.Clients || []) {
    if (!(c.Address + ' ' + c.Protocol).toLowerCase().includes(q)) continue
    const tr = document.createElement('tr')
    for (const value of [c.Address, c.Port, c.Protocol]) {
      const td = document.createElement('td')
      td.textContent = String(value)
      tr.append(td)
    }
    body.append(tr)
  }
  el('empty').hidden = !!body.childElementCount
  el('empty').textContent = q ? 'Совпадений нет' : 'Активных соединений нет'
}
const form = el<HTMLFormElement>('configform')
const input = (name: string) =>
  form.elements.namedItem(name) as HTMLInputElement
const anyAddress = /^(0\.0\.0\.0\/0|0\/0)$/
function checkAllowed() {
  const open = input('Allowed')
    .value.split(/[\s,;]+/)
    .some((value) => anyAddress.test(value))
  const warn = el('aclwarn')
  warn.hidden = !open
  warn.textContent = open
    ? 'Внимание: 0.0.0.0/0 разрешает подключения с любого адреса. Если адрес прослушивания не 127.0.0.1, прокси станет доступен всей локальной сети — включите пароль в разделе «Аутентификация».'
    : ''
}
function statsVisible() {
  return !el('stats').hidden
}
function statsTable(id: string, emptyId: string, list: Stat[]) {
  const body = el(id)
  body.replaceChildren()
  for (const s of list) {
    const tr = document.createElement('tr')
    for (const value of [s.Label, size(s.RX), size(s.TX), size(s.RX + s.TX)]) {
      const td = document.createElement('td')
      td.textContent = value
      tr.append(td)
    }
    body.append(tr)
  }
  el(emptyId).hidden = !!list.length
}
function drawStats(s: Stats) {
  el('statrx').textContent = size(s.Total.RX)
  el('stattx').textContent = size(s.Total.TX)
  el('statsum').textContent = size(s.Total.RX + s.Total.TX)
  el('statsince').textContent = s.Total.Since
    ? 'Учёт с ' +
      new Date(s.Total.Since * 1000).toLocaleString('ru', {
        dateStyle: 'short',
        timeStyle: 'short',
      })
    : 'Учёт ещё не вёлся'
  const days = s.Days || [],
    months = s.Months || []
  statsTable('dayrows', 'daysempty', days)
  statsTable('monthrows', 'monthsempty', months)
  el<HTMLButtonElement>('resetstats').disabled = busy || !(days.length || s.Total.RX)
}
async function refreshStats() {
  try {
    const s: Stats = await call('Stats')
    statsFetched = Date.now()
    drawStats(s)
  } catch (e) {
    message(e)
  }
}
function draw(v: View) {
  view = v
  const c = v.Config,
    active = ['running', 'lost'].includes(v.State)
  el('state').dataset.state = v.State
  el('state').textContent = states[v.State] || v.State
  el<HTMLButtonElement>('start').disabled = busy || active
  el<HTMLButtonElement>('stop').disabled = busy || !active
  el<HTMLButtonElement>('restart').disabled = busy || !active
  el('uptime').textContent = active
    ? new Date(v.Uptime * 1000).toISOString().slice(11, 19)
    : '—'
  el('listeners').textContent = [
    c.HTTP ? 'HTTP ' + c.Bind + ':' + c.HTTP : '',
    c.SOCKS ? 'SOCKS5 ' + c.Bind + ':' + c.SOCKS : '',
  ]
    .filter(Boolean)
    .join('   ·   ')
  el('rx').textContent = size(v.RX)
  el('tx').textContent = size(v.TX)
  const total = v.Totals || { RX: 0, TX: 0, Since: 0 }
  el('total').textContent = size(total.RX + total.TX)
  el('total').title =
    'Принято ' + size(total.RX) + ', передано ' + size(total.TX)
  el('count').textContent = String(
    new Set((v.Clients || []).map((it) => it.Address)).size,
  )
  el('sessions').textContent = 'Соединений: ' + (v.Clients?.length || 0)
  const points = v.History || [],
    p = active ? points.at(-1) : undefined
  el('rate').textContent =
    '↓ ' + size(p?.RX || 0) + '/с   ↑ ' + size(p?.TX || 0) + '/с'
  chart(Number(period.value) ? historic : points)
  rows()
  if (Number(period.value) && Date.now() - fetched > 30000) {
    fetched = Date.now()
    void refreshHistory()
  }
  el('logs').textContent =
    (v.Logs || []).slice().reverse().join('\n') || 'Записей пока нет'
  if (v.Detail) message(v.Detail)
  el('account').textContent = c.User
    ? 'Требуется пароль · ' + c.User
    : 'Доступ по разрешённой подсети'
  el<HTMLButtonElement>('removeuser').disabled = !c.User || busy
  if (statsVisible() && Date.now() - statsFetched > 10000) {
    statsFetched = Date.now()
    void refreshStats()
  }
  if (!initialized) {
    for (const key of [
      'Bind',
      'Allowed',
      'HTTP',
      'SOCKS',
      'DownloadKbps',
      'UploadKbps',
      'ParentAddr',
    ] as const)
      input(key).value = String(c[key])
    el<HTMLSelectElement>('parenttype').value = c.ParentType
    initialized = true
  }
  checkAllowed()
  for (const [id, key] of [
    ['auto', 'AutoStart'],
    ['hidden', 'StartHidden'],
  ] as const) {
    el<HTMLInputElement>(id).checked = c[key]
    el<HTMLInputElement>(id).disabled = busy
  }
}
async function poll() {
  if (polling || busy) return
  polling = true
  const rev = revision
  try {
    const v = await call('Status')
    if (!busy && rev === revision) draw(v)
  } catch (e) {
    message(e)
  } finally {
    polling = false
  }
}
async function action(
  method: string,
  args: unknown[] = [],
  feedback = '',
): Promise<boolean> {
  if (busy) return false
  let ok = false
  busy = true
  revision++
  message('')
  el('saved').textContent = ''
  if (view) draw(view)
  el('state').textContent = (
    {
      Start: 'Запускается…',
      Stop: 'Останавливается…',
      Restart: 'Перезапускается…',
      Save: 'Применяется…',
      Restore: 'Восстанавливается…',
      ResetTraffic: 'Обнуляется…',
      SetCredential: 'Применяется…',
    } as Record<string, string>
  )[method]
  try {
    await call(method, ...args)
    ok = true
    el('saved').textContent = feedback
  } catch (e) {
    message(e)
  } finally {
    busy = false
    try {
      draw(await call('Status'))
    } catch (e) {
      message(e)
    }
  }
  return ok
}
for (const [id, method] of [
  ['start', 'Start'],
  ['stop', 'Stop'],
  ['restart', 'Restart'],
])
  el(id).onclick = () => void action(method)
form.onsubmit = async (e) => {
  e.preventDefault()
  if (!view) return
  const saved = await action(
    'Save',
    [
      {
        ...view.Config,
        Bind: input('Bind').value.trim(),
        Allowed: input('Allowed').value.trim(),
        HTTP: Number(input('HTTP').value),
        SOCKS: Number(input('SOCKS').value),
        DownloadKbps: Number(input('DownloadKbps').value),
        UploadKbps: Number(input('UploadKbps').value),
        ParentAddr: input('ParentAddr').value.trim(),
        ParentType: el<HTMLSelectElement>('parenttype').value,
      },
    ],
    'Сохранено',
  )
  if (saved && view) {
    // Show the subnets as the engine stored them.
    initialized = false
    draw(view)
  }
}
for (const [id, key] of [
  ['auto', 'AutoStart'],
  ['hidden', 'StartHidden'],
])
  el<HTMLInputElement>(id).onchange = (e) => {
    if (view)
      void action('Save', [
        { ...view.Config, [key]: (e.target as HTMLInputElement).checked },
      ])
  }
el<HTMLFormElement>('credentials').onsubmit = (e) => {
  e.preventDefault()
  const password = el<HTMLInputElement>('password')
  void action('SetCredential', [
    el<HTMLInputElement>('username').value,
    password.value,
  ])
  password.value = ''
}
el('removeuser').onclick = () => void action('SetCredential', ['', ''])
async function versions() {
  try {
    const values: string[] = await call('Backups'),
      select = el<HTMLSelectElement>('versions')
    select.replaceChildren(
      ...values.map(
        (name) =>
          new Option(name.slice(0, 8) + ' ' + name.slice(9, 15) + ' UTC', name),
      ),
    )
    if (!values.length) select.add(new Option('Копий пока нет', ''))
    el<HTMLButtonElement>('restore').disabled = !values.length
  } catch (e) {
    message(e)
  }
}
el<HTMLDetailsElement>('backups').ontoggle = (e) => {
  if ((e.target as HTMLDetailsElement).open) void versions()
}
el('restore').onclick = async () => {
  initialized = false
  await action(
    'Restore',
    [el<HTMLSelectElement>('versions').value],
    'Восстановлено',
  )
  await versions()
  if (view) {
    initialized = false
    draw(view)
  }
}
el('search').oninput = rows
input('Allowed').oninput = checkAllowed
el('resetstats').onclick = async () => {
  if (
    !confirm(
      'Обнулить сохранённую статистику трафика? Отменить это действие нельзя.',
    )
  )
    return
  if (await action('ResetTraffic', [], 'Статистика обнулена'))
    await refreshStats()
}
for (const b of document.querySelectorAll<HTMLButtonElement>('nav button'))
  b.onclick = () => {
    for (const s of document.querySelectorAll('main section'))
      (s as HTMLElement).hidden = s.id !== b.dataset.tab
    for (const n of document.querySelectorAll('nav button'))
      n.removeAttribute('aria-current')
    b.setAttribute('aria-current', 'page')
    if (view) chart(Number(period.value) ? historic : view.History || [])
    if (b.dataset.tab === 'stats') void refreshStats()
  }
const theme = el<HTMLSelectElement>('theme')
theme.value = localStorage.getItem('theme') || 'system'
function applyTheme() {
  document.documentElement.style.colorScheme =
    theme.value === 'system' ? 'light dark' : theme.value
}
theme.onchange = () => {
  localStorage.setItem('theme', theme.value)
  applyTheme()
}
applyTheme()
window.addEventListener('resize', () => {
  if (view) chart(Number(period.value) ? historic : view.History || [])
})
void poll()
setInterval(() => {
  if (!document.hidden) void poll()
}, 1000)
