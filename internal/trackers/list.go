package trackers

// services is a curated denylist of email open-tracking pixels.
//
// Sources, merged and trimmed:
//   - DHH / HEY "spy pixels named'n'shamed" (MIT)
//     https://gist.github.com/dhh/360f4dc7ddbce786f8e82b97cdad9d20
//   - Simplify Gmail tracker list (BSD-3-Clause, Michael Leggett)
//     https://github.com/leggett/simplify-trackers
//   - LeaveMeAlone email-trackers (CC-BY 3.0)
//     https://github.com/leavemealone-app/email-trackers
//
// Pattern style: a substring of the lowercased image src. Pick the path or
// subdomain that will not collide with a legitimate asset on the same host.
var services = []struct {
	name     string
	patterns []string
}{
	{"ActiveCampaign", []string{"lt.php?l=open", "lt.php?tid=", "/lt.php?"}},
	{"Adobe", []string{"demdex.net", "t.info.adobesystems.com", "toutapp.com", "sparkpostmail2.com"}},
	{"Amazon", []string{"awstrack.me", "aws-track-email-open", "/gp/r.html", "amazonappservices.com/trk"}},
	{"Apple", []string{"apple.com/report/2/its_mail_sf", "apple_email_link/spacer"}},
	{"AWeber", []string{"openrate.aweber.com"}},
	{"Bananatag", []string{"bl-1.com"}},
	{"Boomerang", []string{"mailstat.us/tr"}},
	{"Campaign Monitor", []string{"cmail1.com/t/", "cmail2.com/t/", "cmail3.com/t/", "createsend1.com/t/"}},
	{"Canary Mail", []string{"canarymail.io/track", "pixels.canarymail.io"}},
	{"Cirrus Insight", []string{"tracking.cirrusinsight.com", "pardot.com/r/"}},
	{"Close", []string{"close.io/email_opened", "close.com/email_opened"}},
	{"Constant Contact", []string{"rs6.net/on.jsp", "constantcontact.com/images/p1x1.gif"}},
	{"ContactMonkey", []string{"contactmonkey.com/api/v1/tracker"}},
	{"ConvertKit", []string{"open.convertkit-mail.com", "convertkit-mail.com/o/", "convertkit-mail2.com/o/"}},
	{"Customer.io", []string{"customeriomail.com/e/o", "track.customer.io/e/o"}},
	{"DidTheyReadIt", []string{"xpostmail.com/t/", "didtheyreadit.com"}},
	{"DotDigital", []string{"trackedlink.net/", "dmtrk.net/open"}},
	{"Emarsys", []string{"emarsys.com/e2t/o/"}},
	{"Facebook", []string{"facebook.com/email/open_tracking", "facebook.com/tr/"}},
	{"Front", []string{"app.frontapp.com/oc/", "web.frontapp.com/oc/"}},
	{"GetMailSpring", []string{"getmailspring.com/open"}},
	{"GetResponse", []string{"getresponse.com/open.html"}},
	{"GitHub", []string{"github.com/notifications/beacon/"}},
	{"GMass", []string{"track.gmass.co", "x.gmtrack.net", "gmass.co/r/"}},
	{"Google", []string{"google.com/appserve/mkt/img/", "ad.doubleclick.net/ddm/ad/", "google-analytics.com/collect"}},
	{"HubSpot", []string{"t.hubspotemail.net", "t.hubspotfree.net", "track.hubspot.com", "t.signaux.co"}},
	{"Hunter", []string{"hunter.io/pixel", "mlnk.io/o/"}},
	{"Intercom", []string{"via.intercom.io/o", "intercom-mail.com/via/o", "via.intercom-mail.com"}},
	{"Klaviyo", []string{"trk.klclick.com", "trk.klclick1.com", "trk.klclick2.com"}},
	{"LinkedIn", []string{"linkedin.com/emimp/"}},
	{"Litmus", []string{"emltrk.com"}},
	{"Mailchimp", []string{"list-manage.com/track"}},
	{"Mailgun", []string{"email.mailgun.net/o/", "email.mg.", "/o/eJw", "track.mailgun.org"}},
	{"Mailjet", []string{"mjt.lu/oo"}},
	{"MailTrack", []string{"mailtrack.io/trace", "mltrk.io/pixel"}},
	{"Mandrill", []string{"mandrillapp.com/track"}},
	{"Marketo", []string{"resources.marketo.com/trk", "marketo.com/trk"}},
	{"MixMax", []string{"email.mixmax.com", "track.mixmax.com"}},
	{"Mixpanel", []string{"api.mixpanel.com/track"}},
	{"Outreach", []string{"outrch.com/api/mailings/opened", "/api/mailings/opened"}},
	{"Polymail", []string{"polymail.io/v2/z", "polymail.io/track"}},
	{"Postmark", []string{"pstmrk.it/open", "pstmrk.it/o/"}},
	{"Salesforce", []string{"nova.collect.igodigital.com", "go.pardot.com/l/", "exct.net/open.aspx"}},
	{"SalesLoft", []string{"salesloft.com/email_trackers"}},
	{"Segment", []string{"email.segment.com/e/o/"}},
	{"SendGrid", []string{"/wf/open?upn=", "/wf/open?", "sendgrid.net/wf/open"}},
	{"Sendinblue", []string{"sendibt1.com", "sendibt2.com", "sendibt3.com", "sendibm1.com", "sendibm2.com"}},
	{"Streak", []string{"mailfoogae.appspot.com"}},
	{"Substack", []string{"email.substack.com/o", "mailgun.substack.com", ".substack.com/o/"}},
	{"Superhuman", []string{"r.superhuman.com"}},
	{"Yesware", []string{"t.yesware.com", "/track/open", "/open.aspx?tp="}},
	{"YAMM", []string{"yamm-track.appspot"}},
}
