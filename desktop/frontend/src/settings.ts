import './style.css'

const toggle = document.querySelector<HTMLInputElement>('#startup')!
const status = document.querySelector<HTMLElement>('#status')!
const service = 'github.com/ashtray01/ashproxy/desktop/settings.Startup.'
const runtimePath = '/wails/runtime.js'
let busy = false

async function sync(write = false) {
  if (busy) return
  busy = true
  toggle.disabled = true
  status.textContent = ''
  try {
    const { Call } = await import(/* @vite-ignore */ runtimePath)
    toggle.checked = write
      ? await Call.ByName(service + 'SetEnabled', toggle.checked)
      : await Call.ByName(service + 'Enabled')
    toggle.disabled = false
  } catch (error) {
    if (write) toggle.checked = !toggle.checked
    status.textContent = String(error)
    toggle.disabled = !write
  } finally {
    busy = false
  }
}

toggle.addEventListener('change', () => void sync(true))
window.addEventListener('focus', () => void sync())
void sync()
