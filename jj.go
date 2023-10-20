package jj

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	isatty "github.com/mattn/go-isatty"
	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

var (
	version = "0.0.1"
	tag     = "jj - JSON Stream Editor " + version
	usage   = `
usage: jj [-v value] [-purnOD] [-i infile] [-o outfile] keypath

examples: jj keypath                      read value from stdin
      or: jj -i infile keypath            read value from infile
      or: jj -v value keypath             edit value
      or: jj -v value -o outfile keypath  edit value and write to outfile

options:
      -v value             Edit JSON key path value
      -p                   Make json pretty, keypath is optional
      -u                   Make json ugly, keypath is optional
      -r                   Use raw values, otherwise types are auto-detected
      -n                   Do not output color or extra formatting
      -O                   Performance boost for value updates
      -D                   Delete the value at the specified key path
      -l                   Output array values on multiple lines
      -i infile            Use input file instead of stdin
      -o outfile           Use output file instead of stdout
      keypath              JSON key path (like "name.last")

for more info: https://github.com/nuvolaris/jj
`
)

type args struct {
	infile    *string
	outfile   *string
	value     *string
	raw       bool
	del       bool
	opt       bool
	keypathok bool
	keypath   string
	pretty    bool
	ugly      bool
	notty     bool
	lines     bool
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s\n", tag)
	if format != "" {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
	fmt.Fprintf(os.Stderr, "%s\n", usage)
}

func help() {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "%s\n", tag)
	fmt.Fprintf(buf, "%s\n", usage)
	os.Stdout.Write(buf.Bytes())
}

func parseArgs() (args, bool, int) {
	var a args
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		default:
			if len(os.Args[i]) > 1 && os.Args[i][0] == '-' {
				for j := 1; j < len(os.Args[i]); j++ {
					switch os.Args[i][j] {
					default:
						fail("unknown option argument: \"-%c\"", os.Args[i][j])
						return a, true, 1
					case '-':
						fail("unknown option argument: \"%s\"", os.Args[i])
						return a, true, 1
					case 'p':
						a.pretty = true
					case 'u':
						a.ugly = true
					case 'r':
						a.raw = true
					case 'O':
						a.opt = true
					case 'D':
						a.del = true
					case 'n':
						a.notty = true
					case 'l':
						a.lines = true
					}
				}
				continue
			}
			if !a.keypathok {
				a.keypathok = true
				a.keypath = os.Args[i]
			} else {
				fail("unknown option argument: \"%s\"", a.keypath)
				return a, true, 1
			}
		case "-v", "-i", "-o":
			arg := os.Args[i]
			i++
			if i >= len(os.Args) {
				fail("argument missing after: \"%s\"", arg)
				return a, true, 1
			}
			switch arg {
			case "-v":
				a.value = &os.Args[i]
			case "-i":
				a.infile = &os.Args[i]
			case "-o":
				a.outfile = &os.Args[i]
			}
		case "--force-notty":
			a.notty = true
		case "--version":
			fmt.Fprintf(os.Stdout, "%s\n", tag)
			return a, false, 0
		case "-h", "--help", "-?":
			help()
			return a, true, 0
		}
	}
	if !a.keypathok && !a.pretty && !a.ugly {
		fail("missing required option: \"keypath\"")
		return a, true, 1
	}
	return a, false, 0
}

func JJMain() (int, error) {
	a, shouldExit, exitCode := parseArgs()
	if shouldExit {
		return exitCode, nil
	}
	var input []byte
	var err error
	var outb []byte
	var outs string
	var outa bool
	var outt gjson.Type
	var f *os.File
	if a.infile == nil {
		input, err = io.ReadAll(os.Stdin)
	} else {
		input, err = os.ReadFile(*a.infile)
	}
	if err != nil {
		goto fail
	}
	if a.del {
		outb, err = sjson.DeleteBytes(input, a.keypath)
		if err != nil {
			goto fail
		}
	} else if a.value != nil {
		raw := a.raw
		val := *a.value
		if !raw {
			switch val {
			default:
				if len(val) > 0 {
					if (val[0] >= '0' && val[0] <= '9') || val[0] == '-' {
						if _, err := strconv.ParseFloat(val, 64); err == nil {
							raw = true
						}
					}
				}
			case "true", "false", "null":
				raw = true
			}
		}
		opts := &sjson.Options{}
		if a.opt {
			opts.Optimistic = true
			opts.ReplaceInPlace = true
		}
		if raw {
			// set as raw block
			outb, err = sjson.SetRawBytesOptions(
				input, a.keypath, []byte(val), opts)
		} else {
			// set as a string
			outb, err = sjson.SetBytesOptions(input, a.keypath, val, opts)
		}
		if err != nil {
			goto fail
		}
	} else {
		if !a.keypathok {
			outb = input
		} else {
			res := gjson.GetBytes(input, a.keypath)
			if a.raw {
				outs = res.Raw
			} else {
				outt = res.Type
				outa = res.IsArray()
				outs = res.String()
			}
		}

	}
	if a.outfile == nil {
		f = os.Stdout
	} else {
		f, err = os.Create(*a.outfile)
		if err != nil {
			goto fail
		}
	}
	if outb == nil {
		outb = []byte(outs)
	}
	if a.lines && outa {
		var outb2 []byte
		gjson.ParseBytes(outb).ForEach(func(_, v gjson.Result) bool {
			outb2 = append(outb2, pretty.Ugly([]byte(v.Raw))...)
			outb2 = append(outb2, '\n')
			return true
		})
		outb = outb2
	} else if a.raw || outt != gjson.String {
		if a.pretty {
			outb = pretty.Pretty(outb)
		} else if a.ugly {
			outb = pretty.Ugly(outb)
		}
	}
	if a.raw && (!a.pretty && !a.ugly) {
		outb = pretty.Pretty(outb)
	}
	if !a.notty && isatty.IsTerminal(f.Fd()) {
		if a.raw || outt != gjson.String {
			outb = pretty.Color(outb, pretty.TerminalStyle)
		} else {
			outb = append([]byte(pretty.TerminalStyle.String[0]), outb...)
			outb = append(outb, pretty.TerminalStyle.String[1]...)
		}
		for len(outb) > 0 && outb[len(outb)-1] == '\n' {
			outb = outb[:len(outb)-1]
		}
		outb = append(outb, '\n')
	}
	if len(outb) > 0 && outb[len(outb)-1] != '\n' {
		outb = append(outb, '\n')
	}
	f.Write(outb)
	f.Close()
	return 0, nil
fail:
	return 1, err
}
