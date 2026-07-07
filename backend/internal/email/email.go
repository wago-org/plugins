// Package email sends transactional mail (e.g. secondary-email verification
// codes) over SMTP using only the standard library. When SMTP is not configured
// it falls back to logging the message, so local development works without a mail
// server. It has no dependencies on the other internal packages.
package email

import (
	"errors"
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

// errHeaderInjection is returned when a header field contains a newline, which
// could otherwise inject additional SMTP headers.
var errHeaderInjection = errors.New("email: header field contains a newline")

// Config holds the SMTP settings, all sourced from the environment.
type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

// Sender delivers plaintext messages via SMTP, with a dev fallback.
type Sender struct{ cfg Config }

// New builds a Sender from config.
func New(cfg Config) *Sender { return &Sender{cfg: cfg} }

// Send delivers a plaintext message. When SMTP is unconfigured (empty Host) it
// logs the recipient + body and returns sent=false, so verification flows still
// work locally. On a real send it returns sent=true.
func (s *Sender) Send(to, subject, body string) (sent bool, err error) {
	if s.cfg.Host == "" {
		log.Printf("email(dev, SMTP unconfigured): to=%s subject=%q body=%q", to, subject, body)
		return false, nil
	}
	port := s.cfg.Port
	if port == "" {
		port = "587"
	}
	from := s.cfg.From
	if from == "" {
		from = s.cfg.User
	}
	// Guard against SMTP header injection: a CR/LF in any header field (notably
	// the user-supplied recipient) would let an attacker inject extra headers.
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") ||
		strings.ContainsAny(from, "\r\n") {
		return false, errHeaderInjection
	}
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		from, to, subject, body))
	auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)
	if err := smtp.SendMail(s.cfg.Host+":"+port, auth, from, []string{to}, msg); err != nil {
		return false, err
	}
	return true, nil
}
