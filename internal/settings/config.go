package settings

import (
	"errors"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// ErrNotObject: the config file holds a string, a number or a boolean: the previous runtime failed to stamp it.
var ErrNotObject = errors.New("settings: the config is not an object")

// ReadConfig is the config file as JSON.parse read it; nil when it is missing, does not parse, or holds null.
func (s *Store) ReadConfig() any {
	v, ok := readJSON(s.ConfigFile)
	if !ok {
		return nil
	}
	return v
}

// Config is getConfig: the file, or a fresh one stamped now when there is none.
func (s *Store) Config() any {
	if v := s.ReadConfig(); v != nil {
		return v
	}
	return jsvalue.NewObject("updatedAt", s.isoNow())
}

// ConfigObject is the config as an object; a config that is not one has no fields.
func (s *Store) ConfigObject() *jsvalue.Object {
	o, _ := s.Config().(*jsvalue.Object)
	if o == nil {
		o = jsvalue.NewObject()
	}
	return o
}

// UpdateConfig is updateConfig: under the config lock, the file's fields with the updates assigned over them.
func (s *Store) UpdateConfig(updates *jsvalue.Object) error {
	return s.ChangeConfig(func(data *jsvalue.Object) { data.Assign(updates) })
}

// ChangeConfig reads the config under its lock, lets change edit it and writes it back. A config that is an array is
// changed as the object it spreads to and written back as the array it is (JavaScript kept the named fields off the
// JSON); one that is a string or a number takes no change and fails to be stamped, unless there is nothing to write.
func (s *Store) ChangeConfig(change func(*jsvalue.Object)) error {
	s.config.Lock()
	defer s.config.Unlock()
	switch data := s.Config().(type) {
	case *jsvalue.Object:
		change(data)
		return s.writeConfig(data, data)
	case []any:
		view := jsvalue.NewObject()
		Spread(view, data)
		change(view)
		return s.writeConfig(view, data)
	default:
		view := jsvalue.NewObject()
		Spread(view, data)
		if s.contentKey(view) == s.written {
			return nil
		}
		return ErrNotObject
	}
}

func (s *Store) contentKey(view *jsvalue.Object) string {
	rest := view.Copy()
	rest.Delete("updatedAt")
	return string(jsvalue.Stringify(rest, ""))
}

// writeConfig is writeConfig: nothing when the content (without updatedAt) is what this process last wrote, else the
// file stamped now, written beside and renamed over.
func (s *Store) writeConfig(view *jsvalue.Object, file any) error {
	key := s.contentKey(view)
	if key == s.written {
		return nil
	}
	if o, ok := file.(*jsvalue.Object); ok {
		o.Set("updatedAt", s.isoNow())
	}
	if err := writeAtomic(s.ConfigFile, jsvalue.Stringify(file, "  "), 0o600); err != nil {
		return err
	}
	s.written = key
	return nil
}

// AutomationToken is getOrCreateAutomationApiToken: the config's token (whatever value a truthy one is), made and
// saved on first use.
func (s *Store) AutomationToken() (any, error) {
	if o, ok := s.ReadConfig().(*jsvalue.Object); ok {
		if v, _ := o.Get("automationApiToken"); jsvalue.Truthy(v) {
			return v, nil
		}
	}
	token := s.Secret()
	return token, s.UpdateConfig(jsvalue.NewObject("automationApiToken", token))
}
