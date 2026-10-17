package changelist

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const DefaultName = "Changes"

type List struct {
	Name    string   `json:"name"`
	Comment string   `json:"comment,omitempty"`
	Files   []string `json:"files,omitempty"`
}

type Store struct {
	Active string  `json:"active"`
	Lists  []*List `json:"lists"`
	path   string
}

func Load(root, gitDir string) *Store {
	s := &Store{path: filepath.Join(gitDir, "ideagit", "changelists.json")}
	if b, err := os.ReadFile(s.path); err == nil && json.Unmarshal(b, s) == nil && len(s.Lists) > 0 {
		s.fix()
		return s
	}
	s.importIntelliJ(root)
	s.fix()
	return s
}

func (s *Store) fix() {
	if len(s.Lists) == 0 {
		s.Lists = []*List{{Name: DefaultName}}
	}
	if s.Get(s.Active) == nil {
		s.Active = s.Lists[0].Name
	}
}

type ijWorkspace struct {
	Components []struct {
		Name  string `xml:"name,attr"`
		Lists []struct {
			Default string `xml:"default,attr"`
			Name    string `xml:"name,attr"`
			Comment string `xml:"comment,attr"`
			Changes []struct {
				After  string `xml:"afterPath,attr"`
				Before string `xml:"beforePath,attr"`
			} `xml:"change"`
		} `xml:"list"`
	} `xml:"component"`
}

func (s *Store) importIntelliJ(root string) {
	b, err := os.ReadFile(filepath.Join(root, ".idea", "workspace.xml"))
	if err != nil {
		return
	}
	var ws ijWorkspace
	if xml.Unmarshal(b, &ws) != nil {
		return
	}
	for _, c := range ws.Components {
		if c.Name != "ChangeListManager" {
			continue
		}
		for _, l := range c.Lists {
			nl := &List{Name: l.Name, Comment: l.Comment}
			for _, ch := range l.Changes {
				p := ch.After
				if p == "" {
					p = ch.Before
				}
				p = strings.TrimPrefix(p, "$PROJECT_DIR$/")
				if p != "" && !strings.HasPrefix(p, "$") {
					nl.Files = append(nl.Files, p)
				}
			}
			s.Lists = append(s.Lists, nl)
			if l.Default == "true" {
				s.Active = l.Name
			}
		}
	}
}

func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

func (s *Store) Get(name string) *List {
	for _, l := range s.Lists {
		if l.Name == name {
			return l
		}
	}
	return nil
}

func (s *Store) Names() []string {
	var res []string
	for _, l := range s.Lists {
		res = append(res, l.Name)
	}
	return res
}

// Assign maps each changed path to a changelist, putting new changes into the active
// list and forgetting paths that are no longer changed. It reports whether anything changed.
func (s *Store) Assign(paths []string) (map[string]string, bool) {
	changed := map[string]bool{}
	for _, p := range paths {
		changed[p] = true
	}
	res := map[string]string{}
	dirty := false
	for _, l := range s.Lists {
		kept := l.Files[:0]
		for _, f := range l.Files {
			if changed[f] && res[f] == "" {
				res[f] = l.Name
				kept = append(kept, f)
			} else {
				dirty = true
			}
		}
		l.Files = kept
	}
	active := s.Get(s.Active)
	for _, p := range paths {
		if res[p] == "" {
			res[p] = active.Name
			if len(s.Lists) > 1 {
				active.Files = append(active.Files, p)
				dirty = true
			}
		}
	}
	return res, dirty
}

func (s *Store) Move(paths []string, to string) {
	target := s.Get(to)
	if target == nil {
		return
	}
	for _, l := range s.Lists {
		l.Files = slices.DeleteFunc(l.Files, func(f string) bool { return slices.Contains(paths, f) })
	}
	target.Files = append(target.Files, paths...)
}

func (s *Store) Add(name, comment string) *List {
	if l := s.Get(name); l != nil {
		return l
	}
	l := &List{Name: name, Comment: comment}
	s.Lists = append(s.Lists, l)
	return l
}

func (s *Store) Rename(old, name, comment string) {
	if l := s.Get(old); l != nil {
		l.Name = name
		l.Comment = comment
		if s.Active == old {
			s.Active = name
		}
	}
}

func (s *Store) Remove(name string) {
	if len(s.Lists) <= 1 {
		return
	}
	l := s.Get(name)
	if l == nil {
		return
	}
	s.Lists = slices.DeleteFunc(s.Lists, func(x *List) bool { return x == l })
	if s.Active == name {
		s.Active = s.Lists[0].Name
	}
	s.Get(s.Active).Files = append(s.Get(s.Active).Files, l.Files...)
}
