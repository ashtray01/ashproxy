# Desktop development

The Windows GUI in desktop/ exposes the current user's login registration
in Settings. Wails 3.1.0 reads and changes the Run value. The GUI queries
registration at startup and on focus. Initial launch performs no write.
A second launch focuses the existing window.

Run npm ci and npm run build in desktop/frontend, then go test ./... and
go build -o ashproxy.exe in desktop. The frontend assets are embedded.

Local checks included building the upstream Windows engine, compiling
TypeScript, bundling the frontend and building the Windows GUI. Windows
UI Automation located the enabled Russian checkbox in the running executable.
An actual logout/login cycle and Windows 10 compatibility still need testing.

The desktop now runs the bundled core under core/3proxy.exe. The engine package
owns startup, shutdown, port ownership checks, configuration and backups. It
processes private telemetry and writes minute totals to SQLite. The frontend
has overview, connections, configuration, log and settings views.

Build the core on a clean checkout with the flags in the desktop workflow.
Copy bin/3proxy.exe into desktop/core before running the integration tests.
The tests start real HTTP and HTTPS destinations, exercise both proxy
protocols, check live counters, authenticate requests and test rollback.
UI Automation extracted the ZIP to a Unicode path with spaces, launched it
against a separate profile, clicked Start, sent HTTP and SOCKS5 requests
through the application, clicked Stop and opened Settings.

The preview is distributed as a CI artifact. The beta tag is reserved for the
remaining acceptance criteria.

## Client subnets, traffic totals and the icon

The allowed clients field takes a list separated by commas, semicolons or
spaces. A bare address becomes /32, every prefix is masked to its network, and
any mask from /0 to /32 is accepted, including 0.0.0.0/0. The engine stores one
canonical string ("192.168.90.0/23, 127.0.0.1/32") and renders it as a single
comma separated `allow *` rule. Tests start the real core with a two-subnet list
and with 0.0.0.0/0.

SQLite keeps three levels now: minute samples for the 30 day graph, a daily
table that is never pruned, and one lifetime row with the time of the last
reset. Opening a database written by the older build backfills days and totals
from the minute table. Stats() reports totals, 62 days and 24 months;
ResetTraffic() clears all three and restarts the visible session counters.
The overview shows the lifetime total and a new Statistics tab shows the day and
month tables with the reset button.

The executable carries the project icon: desktop/assets/app.ico is embedded as
resource 3 through desktop/rsrc_windows_amd64.syso (windres), and the same icon
is the window and tray icon. desktop/tools/make-icon.ps1 rebuilds the PNG, ICO
and SYSO from ../../icon.png; pass -Transparent to drop a flat white background.
The same resource holds the version block: desktop/VERSION is the single source
of the version, and CompanyName (AshSoft Ltd.), ProductName, LegalCopyright and
the contact address are written by the script, so bump VERSION and rerun it
before a release.

## 1.0.0

The project lives in github.com/ashtray01/ashproxy with the main branch; the
module path, service names and the executable are lowercase. The report of a
clean Windows virtual machine confirmed that the packaged ZIP starts, proxies
HTTP and SOCKS5 and restarts without leftovers, so the release is not marked as
a preview. Only the desktop workflow remains: the inherited 3proxy pipelines
were removed with their release guard.

## Still open

- An actual Windows logoff/logon cycle for the Run value and Windows 10
  compatibility; the clean virtual machine covered start, proxy traffic and
  restart, not the login hook.
- UI Automation over the new Statistics tab and the reset confirmation.
- Windows service mode, per-connection control and latency probes.
- Lossless config editing, multi-user management, English localization and
  update checks.
