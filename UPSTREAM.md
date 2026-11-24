# 3proxy baseline

AshProxy desktop development starts at 3proxy 1.0.4, commit
`e28d86d7e415057186c913705150d5a04c55a4fb`. The original source, license
and authors are retained.

Windows builds compiled with ASHPROXY_DESKTOP accept --desktop after the
configuration filename. The parent supplies a private stdin pipe. EOF starts
the upstream shutdown sequence. Once per second the engine writes cumulative
byte counts to stderr. Windows interlocked operations account for simultaneous
HTTP and SOCKS transfers. The desktop hash helper reads the credential from
stdin and uses the upstream BLAKE2b-crypt implementation. Ordinary upstream
builds omit these extensions. See 3proxy.c, common.c, proxy.h and the byte
accounting calls in proxy.c, sockgetchar.c, sockmap.c and udpsockmap.c.

Build from the repository root with a MinGW-w64 toolchain and GNU make.
For w64devkit without LTO, set the environment variables CFLAGS and LDFLAGS
to -O2 before running `make -f Makefile.win`. The resulting engine is
`bin/3proxy.exe`.

## What the tree keeps

Only what the Windows engine needs is vendored: `src/` with the C sources,
headers, the resource script and the plugins and libs directories, the Windows
makefiles (`Makefile.win`, `Makefile.inc`, `src/Makefile`, `src/Makefile.inc`),
`bin/` for the output, `copying`, `authors`, `RELEASE` and the changelogs.
Upstream documentation (doc/, man/, rus.3ps), sample configurations (cfg/),
packaging (debian/, scripts/, Dockerfiles, CMake and the non-Windows
makefiles), the package signing key, the security policy and the Python test
harness live in https://github.com/3proxy/3proxy and are not copied here, so
core updates touch only the sources we actually compile.
