package fetch

import (
	"errors"
	"net/http"
	"net/url"
)

// watchRedirects returns a copy of client that notes, in the returned slice, whether each redirect it follows is
// permanent (301 or 308). The copy keeps the client's transport, timeout and redirect rules, so the guard and the
// proxy checks still decide which redirects are followed; the slice is read once the request is done.
func watchRedirects(client *http.Client) (*http.Client, *[]bool) {
	var permanent []bool
	watched := *client
	watched.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.Response != nil {
			permanent = append(permanent, req.Response.StatusCode == http.StatusMovedPermanently || req.Response.StatusCode == http.StatusPermanentRedirect)
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &watched, &permanent
}

// movedAddress is the address a subscription at address has moved to for good, or "" when the fetch gives no such
// sign: no redirect, a redirect that is not permanent at every step, a final address equal to the one subscribed (the
// fragment aside), or an address with a user name or password on either side, which is never suggested.
func movedAddress(address string, resp *http.Response, permanent []bool) string {
	if len(permanent) == 0 || resp.Request == nil {
		return ""
	}
	for _, ok := range permanent {
		if !ok {
			return ""
		}
	}
	from, err := url.Parse(address)
	if err != nil || from.User != nil {
		return ""
	}
	to := *resp.Request.URL
	if to.User != nil || (to.Scheme != "http" && to.Scheme != "https") {
		return ""
	}
	from.Fragment, to.Fragment = "", ""
	if to.String() == from.String() {
		return ""
	}
	return to.String()
}
