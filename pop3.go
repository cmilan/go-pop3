// Package pop3 provides an implementation of the Post Office
// Protocol - Version 3.
package pop3

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strconv"
	"strings"
)

// Dial connects to the address on the named network.
func Dial(address string) (c *Client, err error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return
	}
	return NewClient(conn)
}

// DialTLS connects to the address on the named network using tls.
func DialTLS(address string) (c *Client, err error) {
	conn, err := tls.Dial("tcp", address, nil)
	if err != nil {
		return
	}
	return NewClient(conn)
}

// NewClient returns a new client object using an existing connection.
func NewClient(conn net.Conn) (c *Client, err error) {
	c = &Client{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}

	// Make sure we receive the server greeting
	line, err := c.ReadLine()
	if err != nil {
		return
	}
	if !IsOK(line) {
		return nil, errors.New("pop3: Server did not respond with +OK")
	}
	return
}

// ReadLine reads a single line from the buffer.
func (c *Client) ReadLine() (line string, err error) {
	b, _, err := c.r.ReadLine()
	if err == io.EOF {
		return
	}
	if err != nil {
		return
	}
	line = string(b)
	return
}

// ReadLines reads from the buffer until it hits the message end dot (".").
func (c *Client) ReadLines() (lines []string, err error) {
	for {
		line, err := c.ReadLine()
		if err != nil {
			return nil, err
		}

		// Look for a dot to indicate the end of a message
		// from the server.
		if line == "." {
			break
		}
		lines = append(lines, line)
	}
	return
}

// Send writes a command to the buffer and flushes it. Does not return any
// lines from the buffer.
func (c *Client) Send(format string, args ...interface{}) (err error) {
	if _, err = c.w.WriteString(fmt.Sprintf(format, args...)); err != nil {
		return
	}
	return c.w.Flush()
}

// Cmd sends a command to the server and returns a single line from the buffer.
func (c *Client) Cmd(format string,
	args ...interface{}) (line string, err error) {
	if err = c.Send(format, args...); err != nil {
		return
	}

	line, err = c.ReadLine()
	if err != nil {
		return
	}
	if !IsOK(line) {
		return "", errors.New("pop3: Server did not respond with +OK")
	}
	return
}

// User sends the username to the server.
func (c *Client) User(u string) (err error) {
	if _, err = c.Cmd("%s %s\r\n", USER, u); err != nil {
		return
	}
	return
}

// Pass sends the password to the server.
func (c *Client) Pass(p string) (err error) {
	if _, err = c.Cmd("%s %s\r\n", PASS, p); err != nil {
		return
	}
	return
}

// Quit sends the quit command to the server and closes the socket.
func (c *Client) Quit() (err error) {
	if err = c.Send("%s\r\n", QUIT); err != nil {
		return
	}
	return c.conn.Close()
}

// Auth sends the username and password to the server using the User and
// Pass methods. Noop is also called incase the server does not respond
// with invalid auth.
func (c *Client) Auth(u, p string) (err error) {
	if err = c.User(u); err != nil {
		return
	}

	if err = c.Pass(p); err != nil {
		return
	}

	// issue a dud command. Server might not respond with invalid auth
	// unless a cmd is issued
	return c.Noop()
}

// Stat retreives a listing for the current maildrop, consisting of the number
// of messages and the total size of the maildrop.
func (c *Client) Stat() (count, size int, err error) {
	line, err := c.Cmd("%s\r\n", STAT)
	if err != nil {
		return
	}

	// Number of messages in maildrop
	count, err = strconv.Atoi(strings.Fields(line)[1])
	if err != nil {
		return
	}
	if count == 0 {
		return
	}

	// Total size of messages in bytes
	size, err = strconv.Atoi(strings.Fields(line)[2])
	if err != nil {
		return
	}
	if size == 0 {
		return
	}
	return
}

// List returns the MessageList object which contains the message non unique
// id and its size.
func (c *Client) List(msg int) (list MessageList, err error) {
	line, err := c.Cmd("%s %s\r\n", LIST, msg)
	if err != nil {
		return
	}

	id, err := strconv.Atoi(strings.Fields(line)[0])
	if err != nil {
		return
	}

	size, err := strconv.Atoi(strings.Fields(line)[1])
	if err != nil {
		return
	}
	return MessageList{id, size}, nil
}

// ListAll returns a MessageList object which contains all messages in the
// maildrop.
func (c *Client) ListAll() (list []MessageList, err error) {
	if _, err = c.Cmd("%s\r\n", LIST); err != nil {
		return
	}

	lines, err := c.ReadLines()
	if err != nil {
		return
	}
	for _, v := range lines {
		id, err := strconv.Atoi(strings.Fields(v)[0])
		if err != nil {
			return nil, err
		}

		size, err := strconv.Atoi(strings.Fields(v)[1])
		if err != nil {
			return nil, err
		}
		list = append(list, MessageList{id, size})
	}
	return
}

// Retr downloads the given message and returns it as a mail.Message object.
func (c *Client) Retr(msg int) (m string, err error) {
	if _, err = c.Cmd("%s %s\r\n", RETR, msg); err != nil {
		return
	}

	m, err = c.r
	if err != nil {
		return
	}

	// mail.ReadMessage does not consume the message end dot in the buffer
	// so we must move the buffer along. Need to find a better way of
	// doing this.
	line, err := c.ReadLine()
	if err != nil {
		return
	}
	if line != "." {
		if err = c.r.UnreadByte(); err != nil {
			return
		}
	}
	return
}

// Dele will delete the given message from the maildrop.
// Changes will only take affect after the Quit command is issued.
func (c *Client) Dele(msg int) (err error) {
	if _, err = c.Cmd("%s %s\r\n", DELE, msg); err != nil {
		return
	}
	return
}

// Noop will do nothing however can prolong the end of a connection.
func (c *Client) Noop() (err error) {
	if _, err = c.Cmd("%s\r\n", NOOP); err != nil {
		return
	}
	return
}

// Rset will unmark any messages that have being marked for deletion in
// the current session.
func (c *Client) Rset() (err error) {
	if _, err = c.Cmd("%s\r\n", RSET); err != nil {
		return
	}
	return
}

// Top will return a varible number of lines for a given message as a
// mail.Message object.
func (c *Client) Top(msg int, n int) (m *mail.Message, err error) {
	if _, err = c.Cmd("%s %d %d\r\n", TOP, msg, n); err != nil {
		return
	}

	m, err = mail.ReadMessage(c.r)
	if err != nil {
		return
	}

	// mail.ReadMessage does not consume the message end dot in the buffer
	// so we must move the buffer along. Need to find a better way of
	// doing this.
	line, err := c.ReadLine()
	if err != nil {

		return
	}
	if line != "." {
		if err = c.r.UnreadByte(); err != nil {
			return
		}
	}
	return
}

// Uidl will return a MessageUidl object which contains the message non
// unique id and a unique id.
func (c *Client) Uidl(msg int) (list MessageUidl, err error) {
	line, err := c.Cmd("%s %s\r\n", UIDL, msg)
	if err != nil {
		return
	}

	id, err := strconv.Atoi(strings.Fields(line)[1])
	if err != nil {
		return
	}
	return MessageUidl{id, strings.Fields(line)[2]}, nil
}

// UidlAll will return a MessageUidl object which contains all messages in
// the maildrop.
func (c *Client) UidlAll() (list []MessageUidl, err error) {
	if _, err = c.Cmd("%s\r\n", UIDL); err != nil {
		return
	}

	lines, err := c.ReadLines()
	if err != nil {
		return
	}
	for _, v := range lines {
		id, err := strconv.Atoi(strings.Fields(v)[0])
		if err != nil {
			return nil, err
		}
		list = append(list, MessageUidl{id, strings.Fields(v)[1]})
	}
	return
}
