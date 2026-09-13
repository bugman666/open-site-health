package alert

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

func (d *Dispatcher) sendMail(ctx context.Context, ev Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.Mailer != nil {
		return d.Mailer(ctx, ev)
	}
	return d.sendSMTP(ev)
}

func (d *Dispatcher) sendSMTP(ev Event) error {
	host := d.cfg.Alert.SMTPHost
	port := d.cfg.Alert.SMTPPort
	if port == 0 {
		port = 587
	}
	from := d.cfg.Alert.SMTPFrom
	if from == "" {
		from = d.cfg.Alert.SMTPUser
	}
	to := d.cfg.Alert.To
	if from == "" || to == "" {
		return fmt.Errorf("smtp from/to missing")
	}

	subject, body := formatMail(ev)
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if d.cfg.Alert.SMTPUser != "" {
		auth = smtp.PlainAuth("", d.cfg.Alert.SMTPUser, d.cfg.Alert.SMTPPass, host)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

func formatMail(ev Event) (subject, body string) {
	subject = fmt.Sprintf("[osh] %s: %s", ev.Kind, ev.URL)
	var b strings.Builder
	fmt.Fprintf(&b, "kind: %s\n", ev.Kind)
	fmt.Fprintf(&b, "url: %s\n", ev.URL)
	fmt.Fprintf(&b, "target_id: %s\n", ev.TargetID)
	if ev.Message != "" {
		fmt.Fprintf(&b, "message: %s\n", ev.Message)
	}
	return subject, b.String()
}
