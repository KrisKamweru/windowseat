package install

import "strings"

// addToUserPath puts dir on the PATH that Windows gives every new terminal
// for this user.
type addToUserPath struct {
	dir string
}

func (s addToUserPath) String() string { return "add " + s.dir + " to your user PATH" }

func (s addToUserPath) run() error {
	return updateUserPath(func(path string) string { return pathWith(path, s.dir) })
}

type removeFromUserPath struct {
	dir string
}

func (s removeFromUserPath) String() string { return "remove " + s.dir + " from your user PATH" }

func (s removeFromUserPath) run() error {
	return updateUserPath(func(path string) string { return pathWithout(path, s.dir) })
}

// pathWith appends dir to a Windows PATH value unless it is already there.
func pathWith(path, dir string) string {
	for _, entry := range strings.Split(path, ";") {
		if samePathEntry(entry, dir) {
			return path
		}
	}
	path = strings.TrimSuffix(path, ";")
	if path == "" {
		return dir
	}
	return path + ";" + dir
}

// pathWithout drops every entry naming dir from a Windows PATH value.
func pathWithout(path, dir string) string {
	var kept []string
	for _, entry := range strings.Split(path, ";") {
		if !samePathEntry(entry, dir) {
			kept = append(kept, entry)
		}
	}
	return strings.Join(kept, ";")
}

func samePathEntry(entry, dir string) bool {
	return strings.EqualFold(strings.TrimRight(entry, `\`), strings.TrimRight(dir, `\`))
}
