package tui

import (
	"fmt"
	"sync"
	"time"

	"mailbox/src/internal/calendar"
	"mailbox/src/internal/config"
	"mailbox/src/internal/contacts"
	"mailbox/src/internal/folders"
	"mailbox/src/internal/format"
	"mailbox/src/internal/mail"
)

// ponytail: global lock, per-connection pools if IMAP throughput matters
type session struct {
	acct *config.Account
	mail *mail.Mail
	cal  *calendar.Cal
	book *contacts.Contacts
	mu   sync.Mutex
}

func openSession() (*session, error) {
	acct, err := config.LoadAccount(false, false)
	if err != nil {
		return nil, err
	}
	m := mail.New(acct)
	if err := m.Connect(); err != nil {
		return nil, err
	}
	s := &session{acct: acct, mail: m}
	if cal, err := calendar.NewCal(acct); err == nil {
		s.cal = cal
	}
	if book, err := contacts.New(acct); err == nil {
		s.book = book
	}
	return s, nil
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mail != nil {
		s.mail.Close()
	}
}

func (s *session) list(folder string, page int) (*mail.Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lim := 50
	return s.mail.ListMessages(folder, false, &lim, page)
}

func (s *session) count(folder string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.CountMessages(folder, false)
}

func (s *session) search(q string, page int) (*mail.Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.Search(mail.NewSearchQuery(q), 50, page, "")
}

func (s *session) thread(folder, uid string) (*mail.ThreadWalk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.Thread(folder, uid)
}

func (s *session) setSeen(folder, uid string, seen bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.mail.SetSeen(folder, uid, seen)
	return err
}

func (s *session) move(folder, uid, dest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.mail.Screen(folder, uid, dest)
	return err
}

func (s *session) clearScreener() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, err := s.mail.ListMessages(folders.SCREENER, false, nil, 1)
	if err != nil {
		return err
	}
	for _, e := range listing.Items {
		if _, err := s.mail.Screen(e.Folder, e.UID, folders.TRASH); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) aside(folder, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.mail.Aside(folder, uid, nil)
	return err
}

func (s *session) labels() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.ListLabels()
}

func (s *session) setLabel(folder, uid, name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.SetLabel(folder, uid, name, on)
}

func (s *session) labeled(name string, page int) (*mail.Listing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.Labeled(name, 50, page)
}

func (s *session) compose(out *mail.Outgoing, draft bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.Compose(out, draft)
}

func (s *session) message(folder, uid string) (*mail.ThreadMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mail.Message(folder, uid)
}

func (s *session) calendars() ([]*format.OM, error) {
	if s.cal == nil {
		return nil, nil
	}
	return s.cal.Calendars()
}

func (s *session) events(start, end time.Time) ([]*format.OM, error) {
	if s.cal == nil {
		return nil, nil
	}
	return s.cal.Events(start.Format("2006-01-02T15:04:05"), end.Format("2006-01-02T15:04:05"), "")
}

func (s *session) todos() ([]*format.OM, error) {
	if s.cal == nil {
		return nil, nil
	}
	return s.cal.Tasks("", "")
}

func (s *session) completeTodo(id string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.CompleteTask(id)
}

func (s *session) addTodo(title string) error {
	if s.cal == nil {
		return nil
	}
	_, err := s.cal.CreateTask(title, "")
	return err
}

func (s *session) uncompleteTodo(id string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.UncompleteTask(id)
}

func (s *session) renameTodo(id, title string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.RenameTask(id, title)
}

func (s *session) deleteTodo(id string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.DeleteTask(id)
}

func (s *session) deleteHabit(id string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.DeleteHabit(id)
}

func (s *session) addEvent(in calendar.EventIn) error {
	if s.cal == nil {
		return fmt.Errorf("calendar not configured")
	}
	_, _, err := s.cal.CreateEvent(in)
	return err
}

func (s *session) updateEvent(id string, in calendar.EventIn) error {
	if s.cal == nil {
		return fmt.Errorf("calendar not configured")
	}
	_, err := s.cal.UpdateEvent(id, in)
	return err
}

func (s *session) deleteEvent(id string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.DeleteEvent(id)
}

func (s *session) createHabit(name, days, color, icon string) error {
	if s.cal == nil {
		return nil
	}
	_, err := s.cal.CreateHabit(name, days, color, icon)
	return err
}

func (s *session) editHabit(id, name, days, color, icon string) error {
	if s.cal == nil {
		return nil
	}
	_, err := s.cal.EditHabit(id, name, days, color, icon, true, true, true, true)
	return err
}

func (s *session) habits(when string) ([]*format.OM, error) {
	if s.cal == nil {
		return nil, nil
	}
	return s.cal.Habits(when)
}

func (s *session) completeHabit(id, when string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.CompleteHabit(id, when)
}

func (s *session) uncompleteHabit(id, when string) error {
	if s.cal == nil {
		return nil
	}
	return s.cal.UncompleteHabit(id, when)
}

func (s *session) contacts() ([]*format.OM, error) {
	if s.book == nil {
		return nil, nil
	}
	return s.book.List()
}

func (s *session) contactShow(id string) (*format.OM, error) {
	if s.book == nil {
		return nil, nil
	}
	return s.book.Show(id)
}

func (s *session) contactSearch(q string) ([]*format.OM, error) {
	if s.book == nil {
		return nil, nil
	}
	return s.book.Search(q)
}

func destIMAP(key string) string {
	switch key {
	case "i", "1":
		return folders.INBOX
	case "d", "2":
		return folders.FEED
	case "p", "3":
		return folders.PAPER_TRAIL
	case "a", "4":
		return folders.ASIDE
	case "t":
		return folders.TRASH
	case "!":
		return folders.JUNK
	case "block", "n":
		return folders.BLOCK
	}
	return ""
}
