package msgraph

import (
	"net/mail"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// GetAddress renders the mailbox's RFC 5322 From value ("First Last <email>"),
// RFC 2047-encoding a non-ASCII display name so it does not reach the
// recipient as mojibake.
func (c *Client) GetAddress() string {
	return c.FromAddress("")
}

// FromAddress is GetAddress with a per-send display name; empty falls back to
// the name the client was built with.
func (c *Client) FromAddress(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(c.FirstName + " " + c.LastName)
	}
	addr := mail.Address{Name: name, Address: c.Email}
	return mailhdr.Address(addr.String())
}
