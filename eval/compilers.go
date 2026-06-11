package eval

import (
	"fmt"
	apipb "github.com/jsannemo/omogenexec/api"
	"github.com/jsannemo/omogenexec/util"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// A Compilation is the result of compiling program sources.
type Compilation struct {
	// This is unset if and only if the Compilation failed.
	Program        *apipb.CompiledProgram
	CompilerErrors string
}

type compileFunc func(program *apipb.Program, outputBase util.FileBase) (*Compilation, error)

func Compile(program *apipb.Program, outputPath string) (*Compilation, error) {
	langs := GetLanguages()
	lang, found := langs[program.Language]
	if !found {
		return nil, fmt.Errorf("could not find language %v", program.Language)
	}
	fb := util.NewFileBase(outputPath)
	fb.GroupWritable = true
	fb.OwnerGid = util.OmogenexecGroupId()
	if err := fb.Mkdir("."); err != nil {
		return nil, fmt.Errorf("failed compile output mkdir %s: %v", outputPath, err)
	}
	for _, file := range program.Sources {
		err := fb.WriteFile(file.Path, file.Contents)
		if err != nil {
			return nil, fmt.Errorf("failed writing source %s: %v", file.Path, err)
		}
	}
	return lang.compile(program, fb)
}

// noCompile represents compilation that only copies some of the source files and uses the given
// run command to execute the program.
func noCompile(runCommandTemplate []string, include func(string) bool, language apipb.LanguageGroup) compileFunc {
	return func(program *apipb.Program, outputBase util.FileBase) (*Compilation, error) {
		var filteredPaths []string
		for _, file := range program.Sources {
			if include(file.Path) {
				filteredPaths = append(filteredPaths, file.Path)
			}
		}
		if len(filteredPaths) == 0 {
			return &Compilation{CompilerErrors: "No valid source files found"}, nil
		}

		runCommand := substituteFiles(runCommandTemplate, filteredPaths)
		return &Compilation{
			Program: &apipb.CompiledProgram{
				ProgramRoot: outputBase.Path(),
				RunCommand:  runCommand,
				Language:    language,
			}}, nil
	}
}

func simpleCompile(compilerPath string, compilerFlags []string, exec []string, filter func(string) bool, language apipb.LanguageGroup) compileFunc {
	return func(program *apipb.Program, outputBase util.FileBase) (*Compilation, error) {
		var filteredPaths []string
		for _, file := range program.Sources {
			if filter(file.Path) {
				filteredPaths = append(filteredPaths, file.Path)
			}
		}
		if err := outputBase.WriteFile("__compiler_input", []byte{}); err != nil {
			return nil, err
		}
		sandboxArgs := sandboxForCompile(outputBase.Path(), language)
		sandbox := newSandbox(0, sandboxArgs)
		err := sandbox.Start()
		if err != nil {
			return nil, fmt.Errorf("couldn't start compilation sandbox: %v", err)
		}
		run, err := sandbox.Run(append([]string{compilerPath}, substituteFiles(compilerFlags, filteredPaths)...))
		sandbox.Finish()
		if err != nil {
			return nil, fmt.Errorf("sandbox failed: %v, %v", err, sandbox.sandboxErr.String())
		}
		stderr, err := outputBase.ReadFile("__compiler_errors")
		if err != nil {
			return nil, fmt.Errorf("could not read compiler errors: %v", err)
		}
		if !run.CrashedWith(0) {
			return &Compilation{
				CompilerErrors: string(stderr),
			}, nil
		}
		return &Compilation{
			Program: &apipb.CompiledProgram{
				ProgramRoot: outputBase.Path(),
				RunCommand:  exec,
				Language:    language,
			}}, nil
	}
}

var rustMainRe = regexp.MustCompile(`(?m)\bfn\s+main\s*\(`)

// findRustCrateRoot returns the single .rs file to hand to rustc, or a non-empty
// error string suitable as a CompilerErrors message. Unlike C++, rustc accepts
// only one input file (the crate root); sibling files are pulled in via `mod`
// declarations against the working directory.
func findRustCrateRoot(sources []*apipb.SourceFile, filter func(string) bool) (string, string) {
	var rsFiles []*apipb.SourceFile
	for _, f := range sources {
		if filter(f.Path) {
			rsFiles = append(rsFiles, f)
		}
	}
	if len(rsFiles) == 0 {
		return "", "No valid source files found"
	}
	if len(rsFiles) == 1 {
		return rsFiles[0].Path, ""
	}
	var mains []string
	for _, f := range rsFiles {
		if rustMainRe.Match(f.Contents) {
			mains = append(mains, f.Path)
		}
	}
	switch len(mains) {
	case 0:
		return "", "No main function found"
	case 1:
		return mains[0], ""
	default:
		return "", "Multiple main functions found"
	}
}

// rustCompile compiles a Rust submission by passing only the crate root to the
// compiler. It is structured like simpleCompile but selects a single input file
// instead of every filtered source.
func rustCompile(compilerPath string, compilerFlags []string, filter func(string) bool, language apipb.LanguageGroup) compileFunc {
	return func(program *apipb.Program, outputBase util.FileBase) (*Compilation, error) {
		crateRoot, errMsg := findRustCrateRoot(program.Sources, filter)
		if errMsg != "" {
			return &Compilation{CompilerErrors: errMsg}, nil
		}
		if err := outputBase.WriteFile("__compiler_input", []byte{}); err != nil {
			return nil, err
		}
		sandboxArgs := sandboxForCompile(outputBase.Path(), language)
		sandbox := newSandbox(0, sandboxArgs)
		err := sandbox.Start()
		if err != nil {
			return nil, fmt.Errorf("couldn't start compilation sandbox: %v", err)
		}
		run, err := sandbox.Run(append([]string{compilerPath}, substituteFiles(compilerFlags, []string{crateRoot})...))
		sandbox.Finish()
		if err != nil {
			return nil, fmt.Errorf("sandbox failed: %v, %v", err, sandbox.sandboxErr.String())
		}
		stderr, err := outputBase.ReadFile("__compiler_errors")
		if err != nil {
			return nil, fmt.Errorf("could not read compiler errors: %v", err)
		}
		if !run.CrashedWith(0) {
			return &Compilation{
				CompilerErrors: string(stderr),
			}, nil
		}
		return &Compilation{
			Program: &apipb.CompiledProgram{
				ProgramRoot: outputBase.Path(),
				RunCommand:  []string{"./executable"},
				Language:    language,
			}}, nil
	}
}

func javaCompile(javacFlags []string, javaFlags []string, filter func(string) bool, language apipb.LanguageGroup) compileFunc {
	return func(program *apipb.Program, outputBase util.FileBase) (*Compilation, error) {
		var filteredPaths []string
		for _, file := range program.Sources {
			if filter(file.Path) {
				filteredPaths = append(filteredPaths, file.Path)
			}
		}
		if err := outputBase.WriteFile("__compiler_input", []byte{}); err != nil {
			return nil, err
		}
		sandboxArgs := sandboxForCompile(outputBase.Path(), language)
		sandbox := newSandbox(0, sandboxArgs)
		err := sandbox.Start()
		if err != nil {
			return nil, fmt.Errorf("couldn't start compilation sandbox: %v", err)
		}
		run, err := sandbox.Run(append([]string{"/usr/bin/javac"}, substituteFiles(javacFlags, filteredPaths)...))
		if err != nil {
			return nil, fmt.Errorf("sandbox failed: %v, %v", err, sandbox.sandboxErr.String())
		}
		stderr, err := outputBase.ReadFile("__compiler_errors")
		if err != nil {
			return nil, fmt.Errorf("could not read compiler errors: %v", err)
		}
		if !run.CrashedWith(0) {
			return &Compilation{
				CompilerErrors: string(stderr),
			}, nil
		}

		var mains []string
		if err := filepath.Walk(outputBase.Path(),
			func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if filepath.Ext(path) == ".class" {
					run, err := sandbox.Run(append([]string{"/usr/bin/javap", path}))
					if err != nil {
						return err
					}
					if run.Crashed() || run.TimedOut() {
						return fmt.Errorf("javap crashed")
					}
					stdout, err := outputBase.ReadFile("__compiler_output")
					if err != nil {
						return err
					}
					stdoutStr := string(stdout)
					if strings.Contains(stdoutStr, "public static void main(java.lang.String[]);") ||
						strings.Contains(stdoutStr, "public static void main(java.lang.String...);") {
						lines := strings.Split(stdoutStr, "\n")
						for _, line := range lines {
							tokens := strings.Split(line, " ")
							if tokens[0] == "class" {
								mains = append(mains, tokens[1])
							} else if tokens[0] == "public" && tokens[1] == "class" {
								mains = append(mains, tokens[2])
							}
						}
					}
				}
				return nil
			}); err != nil {
			return nil, err
		}
		sandbox.Finish()
		if len(mains) == 0 {
			return &Compilation{
				CompilerErrors: "No main function found",
			}, nil
		}
		if len(mains) > 1 {
			return &Compilation{
				CompilerErrors: "Multiple main functions found",
			}, nil
		}
		return &Compilation{
			Program: &apipb.CompiledProgram{
				ProgramRoot: outputBase.Path(),
				RunCommand:  append([]string{"/usr/bin/java"}, substituteMain(javaFlags, mains[0])...),
				Language:    language,
			}}, nil
	}
}

func sandboxForCompile(sourcePath string, language apipb.LanguageGroup) sandboxArgs {
	args := sandboxArgs{
		WorkingDirectory: sourcePath,
		InputPath:        path.Join(sourcePath, "__compiler_input"),
		OutputPath:       path.Join(sourcePath, "__compiler_output"),
		ErrorPath:        path.Join(sourcePath, "__compiler_errors"),
		ExtraWritePaths:  []string{sourcePath},
		TimeLimitMs:      60 * 1000,
		MemoryLimitKb:    1000 * 1000,
		Pids:             30,
		Env:              map[string]string{"TMPDIR": sourcePath},
	}
	setLanguageSandbox(&args, language)
	return args
}

func substituteFiles(args []string, paths []string) []string {
	var newArgs []string
	for _, arg := range args {
		if arg == "{files}" {
			newArgs = append(newArgs, paths...)
		} else {
			newArgs = append(newArgs, arg)
		}
	}
	return newArgs
}

func substituteMain(args []string, main string) []string {
	var newArgs []string
	for _, arg := range args {
		if arg == "{main}" {
			newArgs = append(newArgs, main)
		} else {
			newArgs = append(newArgs, arg)
		}
	}
	return newArgs
}
