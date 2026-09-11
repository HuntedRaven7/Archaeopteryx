package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// JournalOptions controls a journalctl stream.
type JournalOptions struct {
	Unit      string
	Follow    bool
	TailLines int
	Since     string
}

// formatNone is replaced by the default tail count.
const (
	defaultTail = 100
)

// JournalEntry is one decoded journal record.
type JournalEntry struct {
	Timestamp  time.Time
	Hostname   string
	Identifier string
	PID        int
	Message    string
}

// tail returns the number of messages to show, defaulting to a sane size so a
// follow stream does not empty the screen.
func (o JournalOptions) tail() int {
	if o.TailLines <= 0 {
		return defaultTail
	}
	return o.TailLines
}

// journalArgs builds the journalctl argv for the options.
func journalArgs(o JournalOptions) []string {
	args := []string{"-o", "json", "--no-pager", "--quiet",
		"-n", strconv.Itoa(o.tail())}
	if o.Unit != "" {
		args = append(args, "-u", o.Unit)
	}
	if o.Follow {
		args = append(args, "-f")
	}
	if o.Since != "" {
		args = append(args, "--since", o.Since)
	}
	return args
}

// parseJournalLine decodes one `journalctl -o json` line.
func parseJournalLine(line []byte) (JournalEntry, error) {
	var raw map[string]string
	if err := json.Unmarshal(line, &raw); err != nil {
		return JournalEntry{}, err
	}
	var ts time.Time
	usec := raw["_SOURCE_REALTIME_TIMESTAMP"]
	if usec == "" {
		usec = raw["__REALTIME_TIMESTAMP"]
	}
	if i, err := strconv.ParseInt(usec, 10, 64); err == nil {
		ts = time.UnixMicro(i)
	}
	pid, _ := strconv.Atoi(raw["_PID"])
	ident := raw["SYSLOG_IDENTIFIER"]
	if ident == "" {
		ident = raw["_COMM"]
	}
	return JournalEntry{
		Timestamp:  ts,
		Hostname:   raw["_HOSTNAME"],
		Identifier: ident,
		PID:        pid,
		Message:    raw["MESSAGE"],
	}, nil
}

// FormatJournalLine renders an entry like a journalctl text line.
func FormatJournalLine(e JournalEntry) string {
	var b strings.Builder
	if !e.Timestamp.IsZero() {
		b.WriteString(e.Timestamp.Format("2006-01-02T15:04:05.000000-07:00"))
		b.WriteByte(' ')
	}
	if e.Hostname != "" {
		b.WriteString(e.Hostname)
		b.WriteByte(' ')
	}
	if e.Identifier != "" {
		b.WriteString(e.Identifier)
		if e.PID > 0 {
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(e.PID))
			b.WriteByte(']')
		}
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	return b.String()
}

// StreamJournal runs journalctl (streaming `-o json`), decoding each record
// and calling emit for it. The process is killed when ctx is cancelled or emit
// returns an error. journalctl must be present on the node (distroless DDI
// depends on having it in the base image).
func StreamJournal(ctx context.Context, o JournalOptions, emit func(JournalEntry) error) error {
	if _, err := execLookPath("journalctl"); err != nil {
		return fmt.Errorf("journalctl not available: %w", err)
	}
	cmd := execCommand("journalctl", journalArgs(o)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe journalctl: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start journalctl: %w", err)
	}

	stopping := make(chan struct{})
	defer close(stopping)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopping:
			return
		}
		_ = cmd.Process.Kill()
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		entry, err := parseJournalLine(sc.Bytes())
		if err != nil {
			continue
		}
		if err := emit(entry); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if err := sc.Err(); err != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	return nil
}
