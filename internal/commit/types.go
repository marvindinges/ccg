package commit

// CommitType is a Conventional Commits type (e.g. "feat") with a human
// description shown in pickers.
type CommitType struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// DefaultTypes is the built-in Conventional Commits type set (the Angular /
// git-cm convention). Projects may extend or replace this via config.
func DefaultTypes() []CommitType {
	return []CommitType{
		{"feat", "A new feature"},
		{"fix", "A bug fix"},
		{"docs", "Documentation only changes"},
		{"style", "Changes that do not affect meaning (whitespace, formatting)"},
		{"refactor", "A code change that neither fixes a bug nor adds a feature"},
		{"perf", "A code change that improves performance"},
		{"test", "Adding or correcting tests"},
		{"build", "Changes to the build system or dependencies"},
		{"ci", "Changes to CI configuration files and scripts"},
		{"chore", "Other changes that don't modify src or test files"},
		{"revert", "Reverts a previous commit"},
	}
}

// TypeNames returns just the names of the given types, in order.
func TypeNames(types []CommitType) []string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.Name
	}
	return names
}

// HasType reports whether name is present in types.
func HasType(types []CommitType, name string) bool {
	for _, t := range types {
		if t.Name == name {
			return true
		}
	}
	return false
}
