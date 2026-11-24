package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config contains only values that can be represented safely as 3proxy directives.
type Config struct {
	Bind         string
	Allowed      string
	HTTP         int
	SOCKS        int
	AutoStart    bool
	StartHidden  bool
	User         string
	verifier     string
	DownloadKbps int
	UploadKbps   int
	ParentType   string
	ParentAddr   string
}

type storedConfig struct {
	Config
	Verifier string
}

var usernamePattern = regexp.MustCompile("^[A-Za-z0-9._-]{1,32}$")
var verifierPattern = regexp.MustCompile(`^\$3\$[0-9a-f]{32}\$[A-Za-z0-9./]{22}$`)

func defaults() Config {
	return Config{Bind: "127.0.0.1", Allowed: "127.0.0.1/32", HTTP: 3128, SOCKS: 1080}
}

// parseAllowed reads client subnets separated by commas, semicolons or spaces.
// A bare address means a single host, and every prefix is masked to its network.
func parseAllowed(value string) ([]netip.Prefix, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	if len(fields) == 0 {
		return nil, errors.New("Укажите хотя бы одну разрешённую подсеть")
	}
	if len(fields) > 64 {
		return nil, errors.New("Не более 64 подсетей клиентов")
	}
	seen := make(map[netip.Prefix]bool, len(fields))
	list := make([]netip.Prefix, 0, len(fields))
	for _, field := range fields {
		if !strings.Contains(field, "/") {
			field += "/32"
		}
		prefix, err := netip.ParsePrefix(field)
		if err != nil || !prefix.Addr().Is4() {
			return nil, fmt.Errorf("Некорректная подсеть %q: ожидается IPv4/CIDR, например 192.168.90.0/23", field)
		}
		prefix = prefix.Masked()
		if !seen[prefix] {
			seen[prefix] = true
			list = append(list, prefix)
		}
	}
	return list, nil
}

func joinAllowed(list []netip.Prefix) string {
	parts := make([]string, len(list))
	for i, prefix := range list {
		parts[i] = prefix.String()
	}
	return strings.Join(parts, ", ")
}

// allowedRule returns the normalized networks for individual ACL entries.
func allowedRule(value string) []string {
	list, err := parseAllowed(value)
	if err != nil {
		return []string{value}
	}
	parts := make([]string, len(list))
	for i, prefix := range list {
		parts[i] = prefix.String()
	}
	return parts
}

// normalize stores the allowed subnets in one canonical form so that the
// rendered configuration only changes when the subnets really change.
func (c Config) normalize() Config {
	if list, err := parseAllowed(c.Allowed); err == nil {
		c.Allowed = joinAllowed(list)
	}
	return c
}

func (c Config) validate() error {
	if c.ParentType != "" {
		if c.ParentType != "connect" && c.ParentType != "socks5" {
			return errors.New("Неизвестный тип родительского прокси")
		}
		addr, err := netip.ParseAddrPort(c.ParentAddr)
		if err != nil || !addr.Addr().Is4() || addr.Port() == 0 || addr.Addr().IsUnspecified() || addr.Addr().IsMulticast() {
			return errors.New("Адрес родительского прокси должен иметь вид IPv4:порт")
		}
		if addr.Addr().String() == c.Bind && (int(addr.Port()) == c.HTTP || int(addr.Port()) == c.SOCKS) {
			return errors.New("Прокси не может использовать себя как родительский сервер")
		}
	}
	if (c.User != "" || c.verifier != "") && (!usernamePattern.MatchString(c.User) || !verifierPattern.MatchString(c.verifier)) {
		return errors.New("Некорректная учётная запись")
	}
	if c.DownloadKbps < 0 || c.UploadKbps < 0 || c.DownloadKbps > 2000000 || c.UploadKbps > 2000000 {
		return errors.New("Допустимый диапазон скорости: 0–2000000 кбит/с")
	}
	ip, err := netip.ParseAddr(c.Bind)
	if err != nil || !ip.Is4() || !(ip.IsLoopback() || ip.IsPrivate()) {
		return errors.New("Укажите локальный IPv4-адрес или адрес частной сети")
	}
	if _, err = parseAllowed(c.Allowed); err != nil {
		return err
	}
	if c.HTTP < 0 || c.HTTP > 65535 || c.SOCKS < 0 || c.SOCKS > 65535 || c.HTTP == c.SOCKS {
		return errors.New("Порты должны различаться и быть в пределах 1–65535. Ноль отключает протокол")
	}
	return nil
}

func (c Config) render() string {
	var b strings.Builder
	b.WriteString("log\nlogformat \"L%H:%M:%S %N %E %C:%c %R:%r %I %O\"\n")
	if c.User != "" {
		fmt.Fprintf(&b, "users \"%s:CR:%s\"\nauth strong\n", c.User, c.verifier)
	} else {
		b.WriteString("auth iponly\n")
	}
	if c.DownloadKbps > 0 {
		fmt.Fprintf(&b, "bandlimin %d\n", c.DownloadKbps*1000)
	}
	if c.UploadKbps > 0 {
		fmt.Fprintf(&b, "bandlimout %d\n", c.UploadKbps*1000)
	}
	for _, network := range allowedRule(c.Allowed) {
		fmt.Fprintf(&b, "allow * %s\n", network)
		if c.ParentType != "" {
			addr := netip.MustParseAddrPort(c.ParentAddr)
			fmt.Fprintf(&b, "parent 1000 %s %s %d\n", c.ParentType, addr.Addr(), addr.Port())
		}
	}
	b.WriteString("deny *\n")
	if c.HTTP != 0 {
		fmt.Fprintf(&b, "proxy -4 -i%s -p%d\n", c.Bind, c.HTTP)
	}
	if c.SOCKS != 0 {
		fmt.Fprintf(&b, "socks -4 -i%s -p%d\n", c.Bind, c.SOCKS)
	}
	return b.String()
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ash-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func load(dir string) (Config, error) {
	c := defaults()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	data := storedConfig{Config: c}
	if err = json.Unmarshal(b, &data); err != nil {
		return c, err
	}
	c = data.Config.normalize()
	c.verifier = data.Verifier
	return c, c.validate()
}
