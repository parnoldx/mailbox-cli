package calendar

import (
	"fmt"
	"strings"

	"mailbox/src/internal/dav"
	"mailbox/src/internal/format"
	"mailbox/src/internal/vobject"
)

func dueInWindow(due, startsOn, endsOn string) bool {
	if startsOn == "" && endsOn == "" {
		return true
	}
	if due == "" {
		return true
	}
	day := due
	if len(day) >= 10 {
		day = day[:10]
	}
	if startsOn != "" && day < startsOn {
		return false
	}
	if endsOn != "" && day > endsOn {
		return false
	}
	return true
}

const todosQuery = `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VTODO"/>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`

func (cal *Cal) Tasks(startsOn, endsOn string) ([]*format.OM, error) {
	if startsOn != "" {
		if err := requireDate("starts-on date", startsOn); err != nil {
			return nil, err
		}
	}
	if endsOn != "" {
		if err := requireDate("ends-on date", endsOn); err != nil {
			return nil, err
		}
	}
	if startsOn != "" && endsOn != "" && endsOn < startsOn {
		return nil, fmt.Errorf("ends-on %s is before starts-on %s", endsOn, startsOn)
	}
	raw, status, err := cal.client.Report(cal.acct.AufgabenURL, todosQuery, "1")
	if err != nil || (status != 200 && status != 207) {
		return nil, fmt.Errorf("CalDAV report failed: %d", status)
	}
	var rows []*format.OM
	for _, resp := range dav.ParseMultistatus(raw, "calendar-data") {
		props := vobject.Component(resp.Data, "VTODO")
		if props == nil {
			continue
		}
		if strings.EqualFold(vobject.First(props, "STATUS"), "COMPLETED") {
			continue
		}
		statusVal := vobject.First(props, "STATUS")
		if statusVal == "" {
			statusVal = "NEEDS-ACTION"
		}
		due := asLocal(findProp(props, "DUE"), findHas(props, "DUE"))
		if !dueInWindow(due, startsOn, endsOn) {
			continue
		}
		rows = append(rows, format.NewOM(
			"id", shortID(vobject.First(props, "UID")),
			"due", due,
			"status", statusVal,
			"summary", vobject.First(props, "SUMMARY"),
		))
	}
	sortRows(rows, func(a, b *format.OM) bool {
		ka := strOr(a.Get("due")) + "|" + strOr(a.Get("summary"))
		kb := strOr(b.Get("due")) + "|" + strOr(b.Get("summary"))
		return ka < kb
	})
	return rows, nil
}

func (cal *Cal) CreateTask(title, due string) (string, error) {
	uid := newUUID()
	props := []vobject.Prop{
		{Name: "UID", Value: uid},
		{Name: "DTSTAMP", Value: stampUTC()},
		{Name: "SUMMARY", Value: title},
		{Name: "STATUS", Value: "NEEDS-ACTION"},
	}
	if due != "" {
		w, err := parseWhen(due)
		if err != nil {
			return "", err
		}
		if w.isDate {
			props = append(props, vobject.Prop{Name: "DUE", Params: ";VALUE=DATE", Value: w.t.Format("20060102")})
		} else {
			props = append(props, vobject.Prop{Name: "DUE", Value: w.t.UTC().Format("20060102T150405Z")})
		}
	}
	putURL := strings.TrimRight(cal.acct.AufgabenURL, "/") + "/" + uid + ".ics"
	if err := cal.putICS(putURL, "BEGIN:VTODO\r\n"+vobject.Serialize(props)+"END:VTODO"); err != nil {
		return "", err
	}
	return uid, nil
}

type taskHit struct {
	resp  dav.Response
	props []vobject.Prop
	full  string
}

func (cal *Cal) lookupTask(uid string) (taskHit, error) {
	raw, status, err := cal.client.Report(cal.acct.AufgabenURL, todosQuery, "1")
	if err != nil || (status != 200 && status != 207) {
		return taskHit{}, fmt.Errorf("CalDAV report failed: %d", status)
	}
	seen := map[string]bool{}
	var match taskHit
	n := 0
	for _, resp := range dav.ParseMultistatus(raw, "calendar-data") {
		props := vobject.Component(resp.Data, "VTODO")
		full := vobject.First(props, "UID")
		if !uidMatches(full, uid) || seen[full] {
			continue
		}
		seen[full] = true
		n++
		match = taskHit{resp: resp, props: props, full: full}
	}
	if n == 0 {
		return taskHit{}, fmt.Errorf("task not found: %s", uid)
	}
	if n > 1 {
		return taskHit{}, fmt.Errorf("ambiguous task id %q", uid)
	}
	return match, nil
}

func (cal *Cal) taskPutURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return absURL(cal.acct.AufgabenURL, href)
}

func (cal *Cal) CompleteTask(uid string) error {
	hit, err := cal.lookupTask(uid)
	if err != nil {
		return err
	}
	props := dropProp(hit.props, "COMPLETED")
	props = setProp(props, "STATUS", "", "COMPLETED")
	props = append(props, vobject.Prop{Name: "COMPLETED", Value: stampUTC()})
	return cal.putICS(cal.taskPutURL(hit.resp.Href), "BEGIN:VTODO\r\n"+vobject.Serialize(props)+"END:VTODO")
}

func (cal *Cal) UncompleteTask(uid string) error {
	hit, err := cal.lookupTask(uid)
	if err != nil {
		return err
	}
	props := dropProp(hit.props, "COMPLETED")
	props = setProp(props, "STATUS", "", "NEEDS-ACTION")
	return cal.putICS(cal.taskPutURL(hit.resp.Href), "BEGIN:VTODO\r\n"+vobject.Serialize(props)+"END:VTODO")
}

func (cal *Cal) DeleteTask(uid string) error {
	hit, err := cal.lookupTask(uid)
	if err != nil {
		return err
	}
	status, err := cal.client.Delete(cal.taskPutURL(hit.resp.Href))
	if err != nil || (status != 200 && status != 204 && status != 202) {
		return fmt.Errorf("CalDAV delete failed: %d", status)
	}
	return nil
}
