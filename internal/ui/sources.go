package ui

import (
	"path"

	"github.com/razkevich/ideagit/internal/git"
	"github.com/razkevich/ideagit/internal/shelf"
)

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func dirOf(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

func baseOf(p string) string { return path.Base(p) }

func localSource(repo *git.Repo, f git.FileStatus) DiffSource {
	head := repo.HeadHash()
	left := "Base version"
	if head != "" {
		left = short(head) + "  " + f.Path
	}
	right := "Current version"
	if f.Kind == git.Untracked || f.Kind == git.Added {
		left = "Absent"
	}
	if f.Kind == git.Deleted {
		right = "Deleted"
	}
	if f.Kind == git.Conflict {
		left = "Merge base"
		right = "Current version (with conflict markers)"
	}
	return DiffSource{
		Path: f.Path, Header: "local:" + f.Path, LeftTitle: left, RightTitle: right,
		RightEditable: f.Kind != git.Deleted,
		Load: func() ([]byte, []byte, error) {
			var old []byte
			if head != "" && f.Kind != git.Untracked {
				src := f.Path
				if f.OrigPath != "" {
					src = f.OrigPath
				}
				if b, ok := repo.Show("HEAD", src); ok {
					old = b
				}
			}
			cur, _ := repo.ReadWorktree(f.Path)
			if f.Kind == git.Deleted {
				cur = nil
			}
			return old, cur, nil
		},
	}
}

func commitSource(repo *git.Repo, hash string, f git.ChangedFile) DiffSource {
	parent := repo.ParentRev(hash)
	leftName := short(parent)
	if parent == git.EmptyTree {
		leftName = "Absent"
	}
	return DiffSource{
		Path: f.Path, Header: "commit:" + hash + ":" + f.Path,
		LeftTitle: leftName + "  " + firstNonEmpty(f.OrigPath, f.Path), RightTitle: short(hash) + "  " + f.Path,
		Load: func() ([]byte, []byte, error) {
			src := f.Path
			if f.OrigPath != "" {
				src = f.OrigPath
			}
			var old, nw []byte
			if f.Kind != git.Added && parent != git.EmptyTree {
				old, _ = repo.Show(parent, src)
			}
			if f.Kind != git.Deleted {
				nw, _ = repo.Show(hash, f.Path)
			}
			return old, nw, nil
		},
	}
}

func compareSource(repo *git.Repo, rev, label string, f git.ChangedFile) DiffSource {
	return DiffSource{
		Path: f.Path, Header: "cmp:" + rev + ":" + f.Path,
		LeftTitle: label + "  " + firstNonEmpty(f.OrigPath, f.Path), RightTitle: "Current version",
		RightEditable: f.Kind != git.Deleted,
		Load: func() ([]byte, []byte, error) {
			src := firstNonEmpty(f.OrigPath, f.Path)
			old, _ := repo.Show(rev, src)
			cur, _ := repo.ReadWorktree(f.Path)
			return old, cur, nil
		},
	}
}

func stashSource(repo *git.Repo, s git.Stash, f git.ChangedFile) DiffSource {
	rev := s.Hash
	if f.Kind == git.Untracked {
		rev = s.Hash + "^3"
	}
	name := "Stash@{" + itoa(s.Index) + "}"
	return DiffSource{
		Path: f.Path, Header: "stash:" + s.Hash + ":" + f.Path,
		LeftTitle: "Current version  " + f.Path, RightTitle: "🔒" + name,
		LeftEditable: true,
		Load: func() ([]byte, []byte, error) {
			cur, _ := repo.ReadWorktree(f.Path)
			var nw []byte
			if f.Kind != git.Deleted {
				nw, _ = repo.Show(rev, f.Path)
			}
			return cur, nw, nil
		},
	}
}

func shelfSource(repo *git.Repo, s *shelf.Shelf, f *shelf.File) DiffSource {
	return DiffSource{
		Path: f.Path, Header: "shelf:" + s.Name + ":" + f.Path,
		LeftTitle: "Base version  " + f.Path, RightTitle: "Shelved: " + s.Description,
		Load: func() ([]byte, []byte, error) {
			base := f.BaseText(repo)
			sh, err := f.ShelvedText(repo)
			if err != nil {
				return nil, nil, err
			}
			return base, sh, nil
		},
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
