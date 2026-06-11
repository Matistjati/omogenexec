package eval

import (
	"testing"

	apipb "github.com/jsannemo/omogenexec/api"
)

func src(path, contents string) *apipb.SourceFile {
	return &apipb.SourceFile{Path: path, Contents: []byte(contents)}
}

func TestFindRustCrateRoot(t *testing.T) {
	filter := hasExt([]string{".rs"})
	cases := []struct {
		name    string
		sources []*apipb.SourceFile
		want    string
		wantErr string
	}{
		{
			name:    "single file is always the root",
			sources: []*apipb.SourceFile{src("solution.rs", "fn helper() {}")},
			want:    "solution.rs",
		},
		{
			name: "two files, one has fn main",
			sources: []*apipb.SourceFile{
				src("main.rs", "mod helper;\nfn main() { helper::run(); }"),
				src("helper.rs", "pub fn run() {}"),
			},
			want: "main.rs",
		},
		{
			name: "spacing variations still match",
			sources: []*apipb.SourceFile{
				src("entry.rs", "fn   main ()  {}"),
				src("util.rs", "pub fn run() {}"),
			},
			want: "entry.rs",
		},
		{
			name: "two files, no main",
			sources: []*apipb.SourceFile{
				src("a.rs", "pub fn a() {}"),
				src("b.rs", "pub fn b() {}"),
			},
			wantErr: "No main function found",
		},
		{
			name: "two files, two mains",
			sources: []*apipb.SourceFile{
				src("a.rs", "fn main() {}"),
				src("b.rs", "fn main() {}"),
			},
			wantErr: "Multiple main functions found",
		},
		{
			name: "non-rust files are ignored when picking root",
			sources: []*apipb.SourceFile{
				src("main.rs", "fn main() {}"),
				src("notes.txt", "fn main() {} this is not rust"),
			},
			want: "main.rs",
		},
		{
			name:    "no rust files",
			sources: []*apipb.SourceFile{src("notes.txt", "hello")},
			wantErr: "No valid source files found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := findRustCrateRoot(tc.sources, filter)
			if got != tc.want {
				t.Errorf("crate root = %q, want %q", got, tc.want)
			}
			if gotErr != tc.wantErr {
				t.Errorf("error = %q, want %q", gotErr, tc.wantErr)
			}
		})
	}
}
