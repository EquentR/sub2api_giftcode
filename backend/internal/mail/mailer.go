package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SMTPHost       string
	SMTPPort       int
	SMTPUsername   string
	SMTPPassword   string
	FromAddress    string
	AdminToAddress string
	SubjectPrefix  string
}

type Mailer struct {
	cfg Config
}

const (
	smtpBannerProbeTimeout = 3 * time.Second
	smtpOperationTimeout   = 15 * time.Second
)

var errPlainSMTPHandshake = errors.New("smtp plain handshake failed")

func New(cfg Config) *Mailer {
	if strings.TrimSpace(cfg.SubjectPrefix) == "" {
		cfg.SubjectPrefix = "[sub2api-giftcode]"
	}
	return &Mailer{cfg: cfg}
}

func (m *Mailer) SendApprovalEmail(ctx context.Context, to, subject, body string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(to) == "" {
		to = m.cfg.AdminToAddress
	}
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("admin recipient is required")
	}
	msg := strings.NewReplacer("\r", "", "\n", "\r\n").Replace(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.cfg.FromAddress,
		to,
		subject,
		body,
	))
	host := strings.TrimSpace(m.cfg.SMTPHost)
	addr := net.JoinHostPort(host, strconv.Itoa(m.cfg.SMTPPort))
	var auth smtp.Auth
	if username := strings.TrimSpace(m.cfg.SMTPUsername); username != "" {
		auth = smtp.PlainAuth("", username, m.cfg.SMTPPassword, host)
	}
	if err := m.sendViaPlainSMTP(ctx, addr, host, m.cfg.FromAddress, []string{to}, []byte(msg), auth); err != nil {
		if !errors.Is(err, errPlainSMTPHandshake) {
			return err
		}
		if tlsErr := m.sendViaImplicitTLS(ctx, addr, host, m.cfg.FromAddress, []string{to}, []byte(msg), auth); tlsErr != nil {
			return fmt.Errorf("smtp delivery failed: plain=%v; tls=%w", err, tlsErr)
		}
	}
	return nil
}

func (m *Mailer) ApprovalEmail(brandTitle, subjectPrefix string, requestID int64, requestorUsername, requestorEmail, tierLabel string, amount, payAmountCny float64, note, approvalURL string) (string, string) {
	brandTitle = strings.TrimSpace(brandTitle)
	if brandTitle == "" {
		brandTitle = "sub2api"
	}
	subjectPrefix = strings.TrimSpace(subjectPrefix)
	if subjectPrefix == "" {
		subjectPrefix = m.cfg.SubjectPrefix
	}
	if strings.TrimSpace(subjectPrefix) == "" {
		subjectPrefix = fmt.Sprintf("[%s]", brandTitle)
	}
	subject := fmt.Sprintf("%s 兑换申请审批 #%d", subjectPrefix, requestID)
	trimmedNote := strings.TrimSpace(note)
	if trimmedNote == "" {
		trimmedNote = "无"
	}
	tierLabel = strings.TrimSpace(tierLabel)
	if tierLabel == "" {
		tierLabel = fmt.Sprintf("%.0f 美元", amount)
	}
	body := strings.Join([]string{
		brandTitle + " 兑换申请审批",
		"",
		fmt.Sprintf("申请编号: %d", requestID),
		fmt.Sprintf("申请人: %s", requestorUsername),
		fmt.Sprintf("邮箱: %s", requestorEmail),
		fmt.Sprintf("档位: %s", tierLabel),
		fmt.Sprintf("到账金额: %.0f 美元", amount),
		fmt.Sprintf("实付金额: %.0f 人民币", payAmountCny),
		fmt.Sprintf("申请理由: %s", trimmedNote),
		"",
		"请先核对以上信息。打开链接只会进入确认页，不会立即处理申请。",
		"在确认页点击“确认处理申请”后，系统才会按申请方式处理本单。",
		"",
		"审批确认链接:",
		approvalURL,
		"",
		"此链接只能使用一次。",
	}, "\n")
	return subject, body
}

func (m *Mailer) sendViaImplicitTLS(ctx context.Context, addr, host, from string, to []string, msg []byte, auth smtp.Auth) error {
	dialer := &net.Dialer{Timeout: smtpBannerProbeTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	defer conn.Close()

	setSMTPDeadline(conn, ctx, smtpOperationTimeout)

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	return sendSMTPTransaction(client, auth, from, to, msg)
}

func (m *Mailer) sendViaPlainSMTP(ctx context.Context, addr, host, from string, to []string, msg []byte, auth smtp.Auth) error {
	dialer := &net.Dialer{Timeout: smtpBannerProbeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	setSMTPDeadline(conn, ctx, smtpBannerProbeTimeout)

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		if isSMTPHandshakeError(err) {
			return fmt.Errorf("%w: %v", errPlainSMTPHandshake, err)
		}
		return err
	}
	defer client.Close()

	if auth != nil {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
			setSMTPDeadline(conn, ctx, smtpOperationTimeout)
		} else {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
	}

	return sendSMTPTransaction(client, auth, from, to, msg)
}

func sendSMTPTransaction(client *smtp.Client, auth smtp.Auth, from string, to []string, msg []byte) error {
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp: server doesn't support AUTH")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func setSMTPDeadline(conn net.Conn, ctx context.Context, fallback time.Duration) {
	deadline := time.Now().Add(fallback)
	if ctx != nil {
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
	}
	_ = conn.SetDeadline(deadline)
}

func isSMTPHandshakeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "unexpected eof") || strings.Contains(msg, "malformed smtp response")
}
