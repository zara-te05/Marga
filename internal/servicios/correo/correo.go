// Package correo envía los correos del servidor (por ahora, restablecer contraseña).
// Si no hay SMTP configurado no envía nada y el servidor escribe el enlace en su log.
package correo

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Host     string // MARGA_SMTP_HOST
	Puerto   string // MARGA_SMTP_PUERTO: 587 (STARTTLS) o 465 (TLS directo)
	Usuario  string // MARGA_SMTP_USUARIO
	Password string // MARGA_SMTP_PASSWORD
	De       string // MARGA_SMTP_DE, p. ej. "Marga <no-responder@tu-dominio.com>"
}

func (c Config) Activo() bool { return c.Host != "" && c.De != "" }

// Enviar manda un correo de texto plano.
func Enviar(c Config, para, asunto, cuerpo string) error {
	if !c.Activo() {
		return fmt.Errorf("SMTP sin configurar")
	}
	if strings.ContainsAny(para+asunto, "\r\n") {
		return fmt.Errorf("destinatario o asunto inválido")
	}
	puerto := c.Puerto
	if puerto == "" {
		puerto = "587"
	}
	direccion := net.JoinHostPort(c.Host, puerto)
	remitente := c.De
	if i := strings.LastIndex(remitente, "<"); i >= 0 {
		remitente = strings.Trim(remitente[i:], "<> ")
	}
	msg := strings.Join([]string{
		"From: " + c.De,
		"To: " + para,
		"Subject: " + mime.QEncoding.Encode("utf-8", asunto),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(cuerpo, "\n", "\r\n"),
	}, "\r\n")

	var auth smtp.Auth
	if c.Usuario != "" {
		auth = smtp.PlainAuth("", c.Usuario, c.Password, c.Host)
	}
	if puerto != "465" {
		// SendMail usa STARTTLS si el servidor lo ofrece.
		return smtp.SendMail(direccion, auth, remitente, []string{para}, []byte(msg))
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", direccion, &tls.Config{ServerName: c.Host})
	if err != nil {
		return err
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer cl.Close()
	if auth != nil {
		if err := cl.Auth(auth); err != nil {
			return err
		}
	}
	if err := cl.Mail(remitente); err != nil {
		return err
	}
	if err := cl.Rcpt(para); err != nil {
		return err
	}
	wc, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return cl.Quit()
}
