package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hpcloud/tail"
)

var smtpToNotify, smtpSendAs, smtpHost, smtpPort, smtpUser, smtpPass, twilioPhone, twilioSid, twilioToken string
var twilioToNotify []string
var phoneNumberMap map[string]string

func email(from string, to []string, subject string, body string) error {
	if len(to) == 0 {
		return nil
	}

	// Setup headers
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = strings.Join(to, ",")
	headers["Subject"] = subject

	// Setup message
	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	// Connect to the SMTP Server
	servername := fmt.Sprintf("%s:%s", smtpHost, smtpPort)

	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	// TLS config
	tlsconfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         smtpHost,
	}

	// Here is the key, you need to call tls.Dial instead of smtp.Dial
	// for smtp servers running on 465 that require an ssl connection
	// from the very beginning (no starttls)
	conn, err := tls.Dial("tcp", servername, tlsconfig)
	if err != nil {
		return err
	}

	c, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return err
	}

	// Auth
	if err = c.Auth(auth); err != nil {
		return err
	}

	// To && From
	if err = c.Mail(from); err != nil {
		return err
	}

	for _, rcpt := range to {
		if err = c.Rcpt(rcpt); err != nil {
			return err
		}
	}

	// Data
	w, err := c.Data()
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(message))
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	c.Quit()

	return nil
}

func sms(to []string, body string) error {
	urlStr := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", twilioSid)

	for _, toPhone := range to {
		// Pack up the data for our message
		msgData := url.Values{}
		msgData.Set("To", toPhone)
		msgData.Set("From", twilioPhone)
		msgData.Set("Body", body)
		msgDataReader := *strings.NewReader(msgData.Encode())

		// Create HTTP request client
		client := &http.Client{}
		req, _ := http.NewRequest("POST", urlStr, &msgDataReader)
		req.SetBasicAuth(twilioSid, twilioToken)
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		// Make HTTP POST request and return message SID
		resp, _ := client.Do(req)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return errors.New(resp.Status)
		}
	}

	return nil
}

func notifyJoined(username string) error {
	notificationMessage := fmt.Sprintf("%s joined your Minecraft server", username)

	var smsRecipients []string
	if userPhoneNumber, ok := phoneNumberMap[username]; ok {
		for _, targetPhoneNumber := range twilioToNotify {
			if targetPhoneNumber != userPhoneNumber {
				smsRecipients = append(smsRecipients, targetPhoneNumber)
			}
		}
	} else {
		smsRecipients = twilioToNotify
	}

	smsErr := sms(smsRecipients, notificationMessage)
	if smsErr != nil {
		return smsErr
	}

	emailErr := email(smtpSendAs, strings.Split(smtpToNotify, ","), notificationMessage, notificationMessage)
	if emailErr != nil {
		return emailErr
	}

	return nil
}

func main() {
	smtpHost = os.Getenv("SMTP_HOST")
	smtpPort = os.Getenv("SMTP_PORT")
	smtpUser = os.Getenv("SMTP_USER")
	smtpPass = os.Getenv("SMTP_PASS")
	smtpToNotify = os.Getenv("SMTP_TO_NOTIFY")
	smtpSendAs = os.Getenv("SMTP_SEND_AS")
	twilioPhone = os.Getenv("TWILIO_PHONE")
	twilioSid = os.Getenv("TWILIO_SID")
	twilioToken = os.Getenv("TWILIO_TOKEN")
	twilioToNotify = strings.Split(os.Getenv("TWILIO_TO_NOTIFY"), ",")

	phoneMapBytes := []byte(os.Getenv("USERNAME_TO_TWILIO"))
	if len(phoneMapBytes) != 0 {
		if err := json.Unmarshal(phoneMapBytes, &phoneNumberMap); err != nil {
			fmt.Fprintln(os.Stderr, "Error decoding USERNAME_TO_TWILIO:", err)
		}
	}

	joinReg, _ := regexp.Compile(`\[Server thread\/INFO\]: ([a-zA-Z0-9_]{1,16}) joined the game`)
	leftReg, _ := regexp.Compile(`\[Server thread\/INFO\]: ([a-zA-Z0-9_]{1,16}) left the game`)
	durrationAwayToNotifyAt := time.Minute * time.Duration(7)

	lastSeen := map[string]time.Time{}

	ignoreLogsUntil := time.Now().Add(time.Second * time.Duration(5))

	if len(os.Args) != 2 {
		fmt.Println("mcnotify <Minecraft log file to watch>")
		os.Exit(1)
	}

	path := os.Args[1]

	tConfig := tail.Config{
		MustExist: true,
		Follow:    true,
		ReOpen:    true,
		Logger:    tail.DiscardingLogger,
	}

	t, tailErr := tail.TailFile(path, tConfig)
	if tailErr != nil {
		fmt.Println("Failed to tail server Log:", tailErr)
		os.Exit(1)
	}

	fmt.Println("Watching...")
	for line := range t.Lines {
		if line.Time.After(ignoreLogsUntil) {
			joinResult := joinReg.FindStringSubmatch(line.Text)
			leftResult := leftReg.FindStringSubmatch(line.Text)

			if len(joinResult) > 0 {
				username := joinResult[1]
				fmt.Println(username, "Joined!")

				if lastSeen[username].Add(durrationAwayToNotifyAt).Before(time.Now()) {
					fmt.Println("notify of", username)
					notifyErr := notifyJoined(username)
					if notifyErr != nil {
						fmt.Println("WARNING: Failed to notify.", notifyErr)
					}
				}

				lastSeen[username] = line.Time
			}
			if len(leftResult) > 0 {
				username := leftResult[1]
				fmt.Println(username, "Left!")

				lastSeen[username] = line.Time
			}
		}
	}
}
