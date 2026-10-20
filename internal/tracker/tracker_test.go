package tracker

import "testing"

func TestKnown(t *testing.T) {
	for _, address := range []string{
		"https://www.google-analytics.com/collect?v=1&tid=UA-1",
		"https://stats.g.doubleclick.net/pixel.gif",
		"https://pixel.wp.com/g.gif?v=ext",
		"https://stats.wordpress.com/b.gif?host=a.example",
		"https://da.feedsportal.com/r/1/a2.htm",
		"https://feeds.feedburner.com/~r/Blog/~4/abc",
		"https://feedproxy.google.com/~ff/Blog?d=1",
		"https://example.us1.list-manage.com/track/open.php?u=1&id=2",
		"https://mandrillapp.com/track/open.php?u=1",
		"https://u123.ct.sendgrid.net/wf/open?upn=abc",
		"https://mailgun.org/o/abc",
		"https://open.convertkit-mail2.com/abc",
		"https://open.kit-mail3.com/abc",
		"https://pstmrk.it/open/abc",
		"https://track.hubspot.com/__ptq.gif?a=1",
		"https://blog.example.com/__utm.gif?utmwv=1",
		"https://newsletter.example.com/api/v1/email/open/abc.gif",
		"HTTPS://WWW.GOOGLE-ANALYTICS.COM./collect",
	} {
		if !KnownAddress(address) {
			t.Errorf("%s is not known", address)
		}
	}
}

func TestOrdinaryPicturesStay(t *testing.T) {
	for _, address := range []string{
		"https://i0.wp.com/example.com/photo.jpg",
		"https://example.files.wordpress.com/2024/photo.jpg",
		"https://substackcdn.com/image/fetch/w_1456/https%3A%2F%2Fexample.com%2Fa.png",
		"https://substack-post-media.s3.amazonaws.com/public/images/a.png",
		"https://feeds.feedburner.com/Blog",
		"https://example.us1.list-manage.com/images/logo.png",
		"https://mcusercontent.com/abc/images/a.png",
		"https://sendgrid.net/logo.png",
		"https://example.com/photo.jpg?next=/track/open.php",
		"https://notgoogle-analytics.com/a.png",
		"https://example.com/open/photo.png",
		"https://cdn.convertkit.com/a.png",
		"https://open.example.com/a.png",
		"/relative.png",
		"",
	} {
		if KnownAddress(address) {
			t.Errorf("%s is known", address)
		}
	}
	if Known(nil) {
		t.Error("nil is known")
	}
}
