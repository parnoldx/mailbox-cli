// Teams meetings without a calendar entry: POST /me/onlineMeetings mints the
// join link and stops there. Nothing is put on any calendar, so what the link
// gets pasted into is nobody's business here.
package graphdrv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Meet makes a Teams meeting on this account and returns its join link. The
// meeting lives on the Teams server alone: no event is written, nobody is
// invited, and the link works whether or not anything ever references it.
//
// A 403 is the token being older than the OnlineMeetings.ReadWrite scope,
// which only a fresh sign-in mints — so it is said out loud rather than left
// for the caller to decode out of an ErrorAccessDenied.
func (c *Client) Meet(ctx context.Context, subject string, start, end time.Time) (string, error) {
	var out struct {
		JoinWebURL string `json:"joinWebUrl"`
	}
	err := c.do(ctx, request{method: http.MethodPost, path: "/me/onlineMeetings",
		body: jsonBody(map[string]string{
			"subject":       subject,
			"startDateTime": start.UTC().Format(time.RFC3339),
			"endDateTime":   end.UTC().Format(time.RFC3339),
		})}, &out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusForbidden {
		return "", fmt.Errorf("Microsoft 365 refused to make a Teams meeting — " +
			"sign in again so the token carries OnlineMeetings.ReadWrite: `mailbox setup` → repair")
	}
	if err != nil {
		return "", err
	}
	return out.JoinWebURL, nil
}
