// Package shelf reads and writes IntelliJ's shelf format (.idea/shelf/<name>.xml plus
// <name>/shelved.patch) so shelves are shared with the IDE.
package shelf

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/razkevich/ideagit/internal/diff"
	"github.com/razkevich/ideagit/internal/git"
)

type Shelf struct {
	Name        string
	Description string
	Date        time.Time
	Recycled    bool
	Deleted     bool
	XMLPath     string
	PatchPath   string
	Files       []*File
}

type File struct {
	Path     string
	OrigPath string
	Kind     git.ChangeKind
	Base     *string
	BaseRev  string
	Hunks    []diff.Hunk
}

func Dir(repo *git.Repo) string {
	if fi, err := os.Stat(filepath.Join(repo.Root, ".idea")); err == nil && fi.IsDir() {
		return filepath.Join(repo.Root, ".idea", "shelf")
	}
	return filepath.Join(repo.GitDir, "ideagit", "shelf")
}

type xmlChangelist struct {
	XMLName  xml.Name `xml:"changelist"`
	Name     string   `xml:"name,attr"`
	Date     string   `xml:"date,attr"`
	Recycled string   `xml:"recycled,attr"`
	Deleted  string   `xml:"deleted,attr,omitempty"`
	Options  []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"option"`
}

func List(repo *git.Repo) ([]*Shelf, error) {
	dir := Dir(repo)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var res []*Shelf
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
			continue
		}
		s, err := load(repo, filepath.Join(dir, e.Name()))
		if err == nil {
			res = append(res, s)
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Date.After(res[j].Date) })
	return res, nil
}

func load(repo *git.Repo, xmlPath string) (*Shelf, error) {
	b, err := os.ReadFile(xmlPath)
	if err != nil {
		return nil, err
	}
	var x xmlChangelist
	if err := xml.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	s := &Shelf{Name: x.Name, Recycled: x.Recycled == "true", Deleted: x.Deleted == "true", XMLPath: xmlPath}
	if ms, err := strconv.ParseInt(x.Date, 10, 64); err == nil {
		s.Date = time.UnixMilli(ms)
	}
	for _, o := range x.Options {
		switch o.Name {
		case "PATH":
			p := strings.ReplaceAll(o.Value, "$PROJECT_DIR$", repo.Root)
			s.PatchPath = filepath.FromSlash(p)
		case "DESCRIPTION":
			s.Description = o.Value
		}
	}
	if s.Description == "" {
		s.Description = s.Name
	}
	if pb, err := os.ReadFile(s.PatchPath); err == nil {
		s.Files = ParsePatch(string(pb))
	}
	return s, nil
}

func ParsePatch(text string) []*File {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var files []*File
	var cur *File
	subsystem := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "Index: "):
			cur = &File{Path: strings.TrimPrefix(l, "Index: "), Kind: git.Modified}
			files = append(files, cur)
		case cur == nil:
			continue
		case strings.HasPrefix(l, "Subsystem: "):
			subsystem = strings.TrimPrefix(l, "Subsystem: ")
		case strings.HasPrefix(l, "<+>"):
			if strings.HasSuffix(subsystem, "BaseRevisionTextPatchEP") {
				v := unescapeJava(strings.TrimPrefix(l, "<+>"))
				cur.Base = &v
			}
		case strings.HasPrefix(l, "new file mode"):
			cur.Kind = git.Added
		case strings.HasPrefix(l, "deleted file mode"):
			cur.Kind = git.Deleted
		case strings.HasPrefix(l, "rename from "):
			cur.OrigPath = strings.TrimPrefix(l, "rename from ")
			cur.Kind = git.Renamed
		case strings.HasPrefix(l, "--- "):
			name, meta := splitHeader(strings.TrimPrefix(l, "--- "))
			if name == "/dev/null" {
				cur.Kind = git.Added
			} else if strings.HasPrefix(meta, "(revision ") {
				cur.BaseRev = strings.TrimSuffix(strings.TrimPrefix(meta, "(revision "), ")")
			}
		case strings.HasPrefix(l, "+++ "):
			name, _ := splitHeader(strings.TrimPrefix(l, "+++ "))
			if name == "/dev/null" {
				cur.Kind = git.Deleted
			}
		case strings.HasPrefix(l, "@@ "):
			h, ok := diff.ParseHunkHeader(l)
			if !ok {
				continue
			}
			oldLeft, newLeft := h.OldLen, h.NewLen
			for i+1 < len(lines) && (oldLeft > 0 || newLeft > 0 || strings.HasPrefix(lines[i+1], `\`)) {
				i++
				hl := lines[i]
				if hl == "" {
					hl = " "
				}
				switch hl[0] {
				case ' ':
					oldLeft--
					newLeft--
				case '-':
					oldLeft--
				case '+':
					newLeft--
				case '\\':
				default:
					oldLeft, newLeft = 0, 0
					i--
					continue
				}
				h.Lines = append(h.Lines, hl)
			}
			cur.Hunks = append(cur.Hunks, h)
		}
	}
	return files
}

func splitHeader(s string) (string, string) {
	name, meta, _ := strings.Cut(s, "\t")
	name = strings.TrimPrefix(strings.TrimPrefix(name, "a/"), "b/")
	return name, meta
}

func unescapeJava(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteString(`\u`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func escapeJava(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// BaseText returns the content the shelved change was made against.
func (f *File) BaseText(repo *git.Repo) []byte {
	if f.Kind == git.Added {
		return nil
	}
	if f.Base != nil {
		return []byte(*f.Base)
	}
	src := f.Path
	if f.OrigPath != "" {
		src = f.OrigPath
	}
	rev := f.BaseRev
	if rev == "" {
		rev = "HEAD"
	}
	if b, ok := repo.Show(rev, src); ok {
		return b
	}
	b, _ := repo.Show("HEAD", src)
	return b
}

// ShelvedText reconstructs the shelved version by applying the hunks to the base.
func (f *File) ShelvedText(repo *git.Repo) ([]byte, error) {
	if f.Kind == git.Deleted {
		return nil, nil
	}
	base := f.BaseText(repo)
	lines, noEOL := diff.SplitLines(base)
	out, outNoEOL, ok := diff.ApplyHunks(lines, noEOL, f.Hunks)
	if !ok {
		return nil, fmt.Errorf("patch for %s doesn't apply to its base revision", f.Path)
	}
	return diff.JoinLines(out, outNoEOL), nil
}

type Source struct {
	Path     string
	OrigPath string
	Kind     git.ChangeKind
	Old      []byte
	New      []byte
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Create writes a shelf; binary and very large files can't be shelved and are returned
// in skipped so the caller doesn't roll them back.
func Create(repo *git.Repo, description string, sources []Source) (s *Shelf, skipped []string, err error) {
	dir := Dir(repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, skipped, err
	}
	base := strings.Trim(unsafeName.ReplaceAllString(description, "_"), "_")
	if base == "" {
		base = "shelf"
	}
	name := base
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name+".xml")); errors.Is(err, os.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%s%d", base, i)
	}
	now := time.Now()
	head := repo.HeadHash()
	var b strings.Builder
	for _, src := range sources {
		s := src
		r := diff.Compute(s.Old, s.New, diff.IgnoreNone)
		if r.Binary || r.TooLarge {
			skipped = append(skipped, s.Path)
			continue
		}
		fmt.Fprintf(&b, "Index: %s\n", s.Path)
		if s.Kind != git.Added {
			b.WriteString("IDEA additional info:\nSubsystem: com.intellij.openapi.diff.impl.patch.BaseRevisionTextPatchEP\n<+>")
			b.WriteString(escapeJava(string(s.Old)))
			b.WriteString("\n")
		}
		b.WriteString("===================================================================\n")
		oldName := s.Path
		if s.OrigPath != "" {
			oldName = s.OrigPath
		}
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n", oldName, s.Path)
		stamp := fmt.Sprintf("(date %d)", now.UnixMilli())
		rev := fmt.Sprintf("(revision %s)", head)
		switch s.Kind {
		case git.Added, git.Untracked:
			fmt.Fprintf(&b, "new file mode 100644\n--- /dev/null\t%s\n+++ b/%s\t%s\n", stamp, s.Path, stamp)
		case git.Deleted:
			fmt.Fprintf(&b, "deleted file mode 100644\n--- a/%s\t%s\n+++ /dev/null\t%s\n", s.Path, rev, stamp)
		default:
			if s.OrigPath != "" {
				fmt.Fprintf(&b, "rename from %s\nrename to %s\n", s.OrigPath, s.Path)
			}
			fmt.Fprintf(&b, "--- a/%s\t%s\n+++ b/%s\t%s\n", oldName, rev, s.Path, stamp)
		}
		b.WriteString(diff.FormatHunks(r.UnifiedHunks(3)))
	}
	patchDir := filepath.Join(dir, name)
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		return nil, skipped, err
	}
	patchPath := filepath.Join(patchDir, "shelved.patch")
	if err := os.WriteFile(patchPath, []byte(b.String()), 0o644); err != nil {
		return nil, skipped, err
	}
	sh := &Shelf{Name: name, Description: description, Date: now, XMLPath: filepath.Join(dir, name+".xml"), PatchPath: patchPath}
	if err := sh.writeXML(repo); err != nil {
		return nil, skipped, err
	}
	sh.Files = ParsePatch(b.String())
	return sh, skipped, nil
}

func (s *Shelf) writeXML(repo *git.Repo) error {
	rel, err := filepath.Rel(repo.Root, s.PatchPath)
	pathVal := s.PatchPath
	if err == nil && !strings.HasPrefix(rel, "..") {
		pathVal = "$PROJECT_DIR$/" + filepath.ToSlash(rel)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<changelist name=%q date=\"%d\" recycled=\"%t\"", xmlEsc(s.Name), s.Date.UnixMilli(), s.Recycled)
	if s.Deleted {
		b.WriteString(` deleted="true"`)
	}
	b.WriteString(">\n")
	fmt.Fprintf(&b, "  <option name=\"PATH\" value=\"%s\" />\n", xmlEsc(pathVal))
	fmt.Fprintf(&b, "  <option name=\"DESCRIPTION\" value=\"%s\" />\n", xmlEsc(s.Description))
	b.WriteString("</changelist>")
	return os.WriteFile(s.XMLPath, []byte(b.String()), 0o644)
}

func xmlEsc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return strings.ReplaceAll(b.String(), `"`, "&quot;")
}

func (s *Shelf) SetRecycled(repo *git.Repo, v bool) error {
	s.Recycled = v
	return s.writeXML(repo)
}

func (s *Shelf) Rename(repo *git.Repo, description string) error {
	s.Description = description
	return s.writeXML(repo)
}

func (s *Shelf) Remove() error {
	os.Remove(s.XMLPath)
	return os.RemoveAll(filepath.Dir(s.PatchPath))
}

type UnshelveResult struct {
	Applied   []string
	Conflicts []string
}

// Unshelve writes the shelved versions of the given files into the working tree, falling
// back to a three-way merge (with conflict markers) when the file has diverged.
func (s *Shelf) Unshelve(repo *git.Repo, files []*File) (UnshelveResult, error) {
	var res UnshelveResult
	for _, f := range files {
		abs := repo.Abs(f.Path)
		if f.Kind == git.Deleted {
			if cur, ok := repo.ReadWorktree(f.Path); ok && string(cur) != string(f.BaseText(repo)) {
				// Edited since it was shelved; deleting would lose those edits.
				res.Conflicts = append(res.Conflicts, f.Path)
				continue
			}
			os.Remove(abs)
			res.Applied = append(res.Applied, f.Path)
			continue
		}
		shelved, err := f.ShelvedText(repo)
		if err != nil {
			return res, err
		}
		base := f.BaseText(repo)
		current, exists := repo.ReadWorktree(f.Path)
		var out []byte
		switch {
		case !exists || string(current) == string(base):
			out = shelved
		default:
			lines, noEOL := diff.SplitLines(current)
			if applied, appliedNoEOL, ok := diff.ApplyHunks(lines, noEOL, f.Hunks); ok {
				out = diff.JoinLines(applied, appliedNoEOL)
			} else {
				merged, conflict, err := repo.MergeFile(current, base, shelved)
				if err != nil {
					return res, err
				}
				out = merged
				if conflict {
					res.Conflicts = append(res.Conflicts, f.Path)
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(abs, out, 0o644); err != nil {
			return res, err
		}
		if f.OrigPath != "" && f.OrigPath != f.Path {
			if cur, ok := repo.ReadWorktree(f.OrigPath); !ok || string(cur) == string(base) {
				os.Remove(repo.Abs(f.OrigPath))
			}
		}
		res.Applied = append(res.Applied, f.Path)
	}
	return res, nil
}
