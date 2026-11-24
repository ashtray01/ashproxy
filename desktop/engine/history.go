package engine

import (
	"database/sql"
	"errors"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type ledger struct{ db *sql.DB }

const ledgerSchema = `
CREATE TABLE IF NOT EXISTS traffic(minute INTEGER PRIMARY KEY, rx INTEGER NOT NULL, tx INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS daily(day INTEGER PRIMARY KEY, rx INTEGER NOT NULL, tx INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS total(id INTEGER PRIMARY KEY CHECK(id = 1), rx INTEGER NOT NULL, tx INTEGER NOT NULL, since INTEGER NOT NULL);
`
const keepMinutes = 43200 // 30 days of per-minute samples

func openLedger(dir string) (*ledger, error) {
	db, err := sql.Open("sqlite3", filepath.Join(dir, "traffic.sqlite3"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA user_version=2"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(ledgerSchema); err != nil {
		db.Close()
		return nil, err
	}
	l := &ledger{db}
	if err = l.backfill(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec("DELETE FROM traffic WHERE minute < ?", time.Now().Unix()/60-keepMinutes); err != nil {
		db.Close()
		return nil, err
	}
	return l, nil
}

// backfill rebuilds the daily and lifetime totals of a database written by an
// older build, which only kept per-minute samples.
func (l *ledger) backfill() error {
	var days, totals int
	if err := l.db.QueryRow("SELECT COUNT(*) FROM daily").Scan(&days); err != nil {
		return err
	}
	if err := l.db.QueryRow("SELECT COUNT(*) FROM total").Scan(&totals); err != nil {
		return err
	}
	if days > 0 && totals > 0 {
		return nil
	}
	type sample struct{ minute, rx, tx int64 }
	var rows []sample
	query, err := l.db.Query("SELECT minute, rx, tx FROM traffic ORDER BY minute")
	if err != nil {
		return err
	}
	for query.Next() {
		var s sample
		if err = query.Scan(&s.minute, &s.rx, &s.tx); err != nil {
			query.Close()
			return err
		}
		rows = append(rows, s)
	}
	err = query.Err()
	query.Close()
	if err != nil || len(rows) == 0 {
		return err
	}
	type pair struct{ rx, tx int64 }
	byDay := make(map[int]pair)
	var rx, tx int64
	for _, s := range rows {
		day := localDay(s.minute)
		p := byDay[day]
		p.rx += s.rx
		p.tx += s.tx
		byDay[day] = p
		rx += s.rx
		tx += s.tx
	}
	begin, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer begin.Rollback()
	if days == 0 {
		for day, p := range byDay {
			if _, err = begin.Exec("INSERT INTO daily(day, rx, tx) VALUES(?, ?, ?) ON CONFLICT(day) DO UPDATE SET rx=excluded.rx, tx=excluded.tx", day, p.rx, p.tx); err != nil {
				return err
			}
		}
	}
	if totals == 0 {
		if _, err = begin.Exec("INSERT INTO total(id, rx, tx, since) VALUES(1, ?, ?, ?)", rx, tx, rows[0].minute*60); err != nil {
			return err
		}
	}
	return begin.Commit()
}

// localDay is the calendar day of a per-minute sample, as yyyymmdd.
func localDay(minute int64) int {
	t := time.Unix(minute*60, 0).Local()
	return t.Year()*10000 + int(t.Month())*100 + t.Day()
}

type bucket struct {
	ledger *ledger
	minute int64
	rx, tx uint64
}

func (b *bucket) flush() error {
	if b.rx == 0 && b.tx == 0 {
		return nil
	}
	tx, err := b.ledger.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO traffic VALUES(?,?,?) ON CONFLICT(minute) DO UPDATE SET rx=rx+excluded.rx, tx=tx+excluded.tx", b.minute, b.rx, b.tx); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO daily(day, rx, tx) VALUES(?,?,?) ON CONFLICT(day) DO UPDATE SET rx=rx+excluded.rx, tx=tx+excluded.tx", localDay(b.minute), b.rx, b.tx); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO total(id, rx, tx, since) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET rx=rx+excluded.rx, tx=tx+excluded.tx", b.rx, b.tx, time.Now().Unix()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	b.rx, b.tx = 0, 0
	return nil
}

func (b *bucket) record(now time.Time, rx, tx uint64) error {
	minute := now.Unix() / 60
	if minute != b.minute {
		if err := b.flush(); err != nil {
			return err
		}
		if minute/1440 != b.minute/1440 {
			if _, err := b.ledger.db.Exec("DELETE FROM traffic WHERE minute < ?", minute-keepMinutes); err != nil {
				return err
			}
		}
		b.minute = minute
	}
	b.rx += rx
	b.tx += tx
	return nil
}

func (m *Manager) Traffic(minutes int) ([]Sample, error) {
	switch minutes {
	case 5, 60, 1440, 10080, 43200:
	default:
		return nil, errors.New("Недопустимый период истории")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.openHistory(); err != nil {
		return nil, err
	}
	if s := m.current; s != nil {
		s.mu.Lock()
		err := s.bucket.flush()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	now := time.Now().Unix() / 60
	first := now - int64(minutes) + 1
	width := (minutes + 719) / 720
	rows, err := m.ledger.db.Query("SELECT (minute-?)/?, SUM(rx), SUM(tx) FROM traffic WHERE minute BETWEEN ? AND ? GROUP BY 1", first, width, first, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]Sample, (minutes+width-1)/width)
	for rows.Next() {
		var i int
		var rx, tx float64
		if err := rows.Scan(&i, &rx, &tx); err != nil {
			return nil, err
		}
		points[i] = Sample{rx / float64(width*60), tx / float64(width*60)}
	}
	return points, rows.Err()
}

type Totals struct {
	RX, TX uint64
	Since  int64 // unix time of the last reset
}

// Stat is one row of the day or month table.
type Stat struct {
	Label  string
	RX, TX uint64
}

type Stats struct {
	Total  Totals
	Days   []Stat
	Months []Stat
}

// Stats reports the lifetime counters and the day and month tables.
func (m *Manager) Stats() (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.openHistory(); err != nil {
		return Stats{}, err
	}
	if s := m.current; s != nil {
		s.mu.Lock()
		err := s.bucket.flush()
		s.mu.Unlock()
		if err != nil {
			return Stats{}, err
		}
	}
	return m.ledger.stats()
}

func (l *ledger) stats() (Stats, error) {
	var v Stats
	err := l.db.QueryRow("SELECT rx, tx, since FROM total WHERE id = 1").Scan(&v.Total.RX, &v.Total.TX, &v.Total.Since)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	days, err := l.db.Query("SELECT day, rx, tx FROM daily ORDER BY day DESC LIMIT 62")
	if err != nil {
		return v, err
	}
	defer days.Close()
	for days.Next() {
		var day int
		var s Stat
		if err = days.Scan(&day, &s.RX, &s.TX); err != nil {
			return v, err
		}
		s.Label = time.Date(day/10000, time.Month(day/100%100), day%100, 0, 0, 0, 0, time.Local).Format("2006-01-02")
		v.Days = append(v.Days, s)
	}
	if err = days.Err(); err != nil {
		return v, err
	}
	months, err := l.db.Query("SELECT day/100, SUM(rx), SUM(tx) FROM daily GROUP BY 1 ORDER BY 1 DESC LIMIT 24")
	if err != nil {
		return v, err
	}
	defer months.Close()
	for months.Next() {
		var month int
		var s Stat
		if err = months.Scan(&month, &s.RX, &s.TX); err != nil {
			return v, err
		}
		s.Label = time.Date(month/100, time.Month(month%100), 1, 0, 0, 0, 0, time.Local).Format("2006-01")
		v.Months = append(v.Months, s)
	}
	return v, months.Err()
}

// ResetTraffic clears the saved counters; the proxy keeps running.
func (m *Manager) ResetTraffic() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.openHistory(); err != nil {
		return err
	}
	if s := m.current; s != nil {
		s.mu.Lock()
		err := s.bucket.flush()
		if err == nil {
			s.baseRX, s.baseTX = s.rx, s.tx
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if _, err := m.ledger.db.Exec("DELETE FROM traffic; DELETE FROM daily;"); err != nil {
		return err
	}
	_, err := m.ledger.db.Exec("INSERT INTO total(id, rx, tx, since) VALUES(1, 0, 0, ?) ON CONFLICT(id) DO UPDATE SET rx=0, tx=0, since=excluded.since", time.Now().Unix())
	return err
}

// totals returns the lifetime counters for the status snapshot.
func (m *Manager) totals() Totals {
	if m.ledger == nil {
		if err := m.openHistory(); err != nil {
			return Totals{}
		}
	}
	var t Totals
	if err := m.ledger.db.QueryRow("SELECT rx, tx, since FROM total WHERE id = 1").Scan(&t.RX, &t.TX, &t.Since); err != nil {
		return Totals{}
	}
	return t
}
