package planner

import (
	"go/ast"
	"path"
	"strconv"
	"strings"
	"unicode"

	"gophermind/gophermind-lib/build/contract"
)

// The executor writes a leaf's reply whole into the leaf's file, so a function
// needs a file of its own: not shared with another function (the later swap
// would overwrite the earlier one) and not shared with a type (the types file
// is written first and a swap would destroy the declarations). One shared rule,
// used by the contract stage on the decoded doc and by the executor on a saved
// contract.

// ffItem is one declaration reduced to what the rule needs.
type ffItem struct {
	id, file, sig string
}

// snakeName is name in snake_case: a boundary falls between a lower case
// letter or digit and an upper case letter, and before the last capital of an
// acronym that is followed by a lower case letter.
func snakeName(name string) string {
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		switch {
		case unicode.IsLetter(c) || unicode.IsDigit(c):
			b.WriteRune(unicode.ToLower(c))
		case c == '_':
			b.WriteByte('_')
		}
	}
	return b.String()
}

// funcFileBase is the file name (no directory, no extension) for a function:
// its snake_case name, or <receiver>_<method> for a method. A signature that
// does not parse falls back to the id.
func funcFileBase(it ffItem) string {
	base := ""
	if fd, err := parseSignature(it.sig); err == nil {
		base = snakeName(fd.Name.Name)
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			t := fd.Recv.List[0].Type
			for {
				switch x := t.(type) {
				case *ast.StarExpr:
					t = x.X
					continue
				case *ast.ParenExpr:
					t = x.X
					continue
				case *ast.IndexExpr:
					t = x.X
					continue
				case *ast.IndexListExpr:
					t = x.X
					continue
				}
				break
			}
			if id, ok := t.(*ast.Ident); ok {
				base = snakeName(id.Name) + "_" + base
			}
		}
	}
	if base == "" {
		base = snakeName(strings.TrimPrefix(strings.ReplaceAll(it.id, "-", "_"), "fn_"))
	}
	if base == "" {
		base = "func"
	}
	if strings.HasSuffix(base, "_test") {
		base += "_fn"
	}
	if strings.HasPrefix(base, "zz_gm") {
		base = "fn_" + base
	}
	return base
}

// uniqueFunctionFiles returns the new repo-relative file of each function whose
// file is shared with another function or with a type. Files are repo-relative
// (see RepoFile). A function with a file of its own is not in the result.
func uniqueFunctionFiles(types []string, fns []ffItem) map[string]string {
	typeFile := map[string]bool{}
	for _, f := range types {
		typeFile[f] = true
	}
	count := map[string]int{}
	for _, f := range fns {
		count[f.file]++
	}
	// taken holds every file that stays as it is.
	taken := map[string]bool{}
	for f := range typeFile {
		taken[f] = true
	}
	for _, f := range fns {
		if f.file != "" && count[f.file] == 1 && !typeFile[f.file] {
			taken[f.file] = true
		}
	}
	out := map[string]string{}
	for _, f := range fns {
		if f.file == "" || (count[f.file] == 1 && !typeFile[f.file]) {
			continue
		}
		dir, base := path.Dir(f.file), funcFileBase(f)
		cand := path.Join(dir, base+".go")
		for n := 2; taken[cand]; n++ {
			cand = path.Join(dir, base+"_"+strconv.Itoa(n)+".go")
		}
		taken[cand] = true
		if cand != f.file {
			out[f.id] = cand
		}
	}
	return out
}

// UniqueFunctionFiles gives every function of c a file that no other function
// and no type uses (see uniqueFunctionFiles) and returns the old file of each
// function it moved, by id. The files of c must be repo-relative
// (NormalizeContractFiles). The directory, and so the package, is unchanged.
func UniqueFunctionFiles(c *contract.Contracts) map[string]string {
	var types []string
	for _, t := range c.Types {
		types = append(types, t.File)
	}
	var fns []ffItem
	for _, f := range c.Functions {
		fns = append(fns, ffItem{id: f.ID, file: f.File, sig: f.Signature})
	}
	moved := uniqueFunctionFiles(types, fns)
	old := map[string]string{}
	for i := range c.Functions {
		f := &c.Functions[i]
		if nf, ok := moved[f.ID]; ok {
			old[f.ID] = f.File
			f.File = nf
		}
	}
	return old
}

// uniqueFunctionFilesDoc is UniqueFunctionFiles on the decoded contract doc.
// It returns the ids of the functions it moved, in contract order.
func uniqueFunctionFilesDoc(doc map[string]any) []string {
	var types []string
	for _, o := range objects(doc["types"]) {
		f, _ := o["file"].(string)
		types = append(types, f)
	}
	fo := objects(doc["functions"])
	var fns []ffItem
	for _, o := range fo {
		id, _ := o["id"].(string)
		file, _ := o["file"].(string)
		sig, _ := o["signature"].(string)
		fns = append(fns, ffItem{id: id, file: file, sig: sig})
	}
	moved := uniqueFunctionFiles(types, fns)
	var ids []string
	for _, o := range fo {
		id, _ := o["id"].(string)
		if nf, ok := moved[id]; ok {
			o["file"] = nf
			ids = append(ids, id)
		}
	}
	return ids
}
