package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type idl struct {
	Address      string           `json:"address"`
	Metadata     idlMetadata      `json:"metadata"`
	Instructions []idlInstruction `json:"instructions"`
	Accounts     []idlAccountDef  `json:"accounts"`
	Types        []idlTypeDef     `json:"types"`
	Errors       []idlError       `json:"errors"`
}

type idlMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type idlInstruction struct {
	Name          string            `json:"name"`
	Docs          []string          `json:"docs"`
	Discriminator []int             `json:"discriminator"`
	Accounts      []idlInstrAccount `json:"accounts"`
	Args          []idlArg          `json:"args"`
}

type idlInstrAccount struct {
	Name      string   `json:"name"`
	Writable  bool     `json:"writable"`
	Signer    bool     `json:"signer"`
	PDA       *idlPDA  `json:"pda"`
	Address   string   `json:"address"`
	Relations []string `json:"relations"`
}

type idlPDA struct {
	Seeds   []idlSeed      `json:"seeds"`
	Program *idlPDAProgram `json:"program"`
}

type idlSeed struct {
	Kind  string `json:"kind"`
	Value []int  `json:"value"`
	Path  string `json:"path"`
}

type idlPDAProgram struct {
	Kind  string `json:"kind"`
	Value []int  `json:"value"`
	Path  string `json:"path"`
}

type idlArg struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

type idlAccountDef struct {
	Name          string          `json:"name"`
	Discriminator []int           `json:"discriminator"`
	Type          json.RawMessage `json:"type"`
}

type idlTypeDef struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

type idlTypeDesc struct {
	Kind   string            `json:"kind"`
	Fields []json.RawMessage `json:"fields"`
}

type idlTypeField struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

type idlError struct {
	Code uint32 `json:"code"`
	Name string `json:"name"`
	Msg  string `json:"msg"`
}

type typeRef struct {
	Kind    string
	Elem    *typeRef
	Len     int
	Defined string
}

func main() {
	idlPath := flag.String("idl", "", "path to IDL json")
	outDir := flag.String("out", "", "output directory")
	pkgName := flag.String("pkg", "", "package name")
	flag.Parse()

	if *idlPath == "" || *outDir == "" || *pkgName == "" {
		fail("idl, out, and pkg flags are required")
	}

	raw, err := os.ReadFile(*idlPath)
	if err != nil {
		fail("read idl: %v", err)
	}

	var doc idl
	if err := json.Unmarshal(raw, &doc); err != nil {
		fail("parse idl: %v", err)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail("mkdir out: %v", err)
	}

	writeFile(*outDir, "program.go", generateProgram(*pkgName, doc))
	writeFile(*outDir, "types.go", generateTypes(*pkgName, doc))
	writeFile(*outDir, "accounts.go", generateAccounts(*pkgName, doc))
	writeFile(*outDir, "instructions.go", generateInstructions(*pkgName, doc))
	writeFile(*outDir, "errors.go", generateErrors(*pkgName, doc))
}

func writeFile(outDir, name, content string) {
	formatted, err := format.Source([]byte(content))
	if err != nil {
		fail("format %s: %v", name, err)
	}
	target := filepath.Join(outDir, name)
	if err := os.WriteFile(target, formatted, 0o644); err != nil {
		fail("write %s: %v", target, err)
	}
	fmt.Printf("generated %s\n", target)
}

func generateProgram(pkg string, doc idl) string {
	var b strings.Builder
	header(&b, pkg)
	b.WriteString("import \"github.com/gagliardetto/solana-go\"\n\n")
	b.WriteString("const ProgramID string = \"" + doc.Address + "\"\n")
	b.WriteString("const ProgramName string = \"" + doc.Metadata.Name + "\"\n")
	b.WriteString("const ProgramVersion string = \"" + doc.Metadata.Version + "\"\n")
	b.WriteString("var ProgramKey = solana.MustPublicKeyFromBase58(ProgramID)\n")
	return b.String()
}

func generateTypes(pkg string, doc idl) string {
	var b strings.Builder
	header(&b, pkg)

	imports := map[string]struct{}{}
	for _, t := range doc.Types {
		desc, _ := parseTypeDesc(t.Type)
		if desc == nil || desc.Kind != "struct" {
			continue
		}
		for _, f := range typeFields(desc) {
			collectImports(parseType(f.Type), imports)
		}
	}
	if len(imports) > 0 {
		b.WriteString("import (\n")
		if _, ok := imports["solana"]; ok {
			b.WriteString("\t\"github.com/gagliardetto/solana-go\"\n")
		}
		if _, ok := imports["bin"]; ok {
			b.WriteString("\tbin \"github.com/gagliardetto/binary\"\n")
		}
		b.WriteString(")\n\n")
	}

	for _, t := range doc.Types {
		desc, _ := parseTypeDesc(t.Type)
		if desc == nil || desc.Kind != "struct" {
			continue
		}
		b.WriteString("type " + toExport(t.Name) + " struct {\n")
		for _, f := range typeFields(desc) {
			tr := parseType(f.Type)
			tag := f.Name
			if tr.Kind == "option" {
				tag += " optional"
			}
			b.WriteString("\t" + toExport(f.Name) + " " + goType(tr) + " `bin:\"" + tag + "\"`\n")
		}
		b.WriteString("}\n\n")
	}
	return b.String()
}

func generateAccounts(pkg string, doc idl) string {
	var b strings.Builder
	header(&b, pkg)
	b.WriteString("import (\n\t\"bytes\"\n\t\"fmt\"\n\n\tbin \"github.com/gagliardetto/binary\"\n\t\"github.com/gagliardetto/solana-go\"\n)\n\n")

	// map for quick lookup of defined struct presence
	typeNames := map[string]bool{}
	for _, t := range doc.Types {
		typeNames[t.Name] = true
	}

	for _, acc := range doc.Accounts {
		disc := bytesLiteral(acc.Discriminator)
		b.WriteString("var " + toExport(acc.Name) + "Discriminator = " + disc + "\n\n")

		// Ensure type exists
		if !typeNames[acc.Name] {
			b.WriteString("type " + toExport(acc.Name) + " struct{}\n\n")
		}

		b.WriteString("func (a *" + toExport(acc.Name) + ") Unmarshal(data []byte) error {\n")
		b.WriteString("\tif len(data) < 8 {\n\t\treturn fmt.Errorf(\"account " + acc.Name + ": data too short\")\n\t}\n")
		b.WriteString("\tif !bytes.Equal(data[:8], " + toExport(acc.Name) + "Discriminator) {\n\t\treturn fmt.Errorf(\"account " + acc.Name + ": discriminator mismatch\")\n\t}\n")
		b.WriteString("\tdec := bin.NewBorshDecoder(data[8:])\n")
		b.WriteString("\treturn dec.Decode(a)\n")
		b.WriteString("}\n\n")

		b.WriteString("func (a *" + toExport(acc.Name) + ") Address(pubkey solana.PublicKey) solana.PublicKey {\n\treturn pubkey\n}\n\n")
	}
	return b.String()
}

func generateInstructions(pkg string, doc idl) string {
	var b strings.Builder
	header(&b, pkg)

	hasArgSeed := false
	for _, ins := range doc.Instructions {
		for _, acc := range ins.Accounts {
			if acc.PDA == nil {
				continue
			}
			for _, seed := range acc.PDA.Seeds {
				if seed.Kind == "arg" {
					hasArgSeed = true
					break
				}
			}
		}
	}

	b.WriteString("import (\n")
	b.WriteString("\t\"bytes\"\n")
	if hasArgSeed {
		b.WriteString("\t\"encoding/binary\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\n\tbin \"github.com/gagliardetto/binary\"\n")
	b.WriteString("\t\"github.com/gagliardetto/solana-go\"\n")
	b.WriteString(")\n\n")

	for _, ins := range doc.Instructions {
		disc := bytesLiteral(ins.Discriminator)
		b.WriteString("var " + toExport(ins.Name) + "Discriminator = " + disc + "\n\n")

		// Args struct
		if len(ins.Args) > 0 {
			b.WriteString("type " + toExport(ins.Name) + "Args struct {\n")
			for _, arg := range ins.Args {
				tr := parseType(arg.Type)
				tag := arg.Name
				if tr.Kind == "option" {
					tag += " optional"
				}
				b.WriteString("\t" + toExport(arg.Name) + " " + goType(tr) + " `bin:\"" + tag + "\"`\n")
			}
			b.WriteString("}\n\n")
		} else {
			b.WriteString("type " + toExport(ins.Name) + "Args struct{}\n\n")
		}

		// Accounts struct
		b.WriteString("type " + toExport(ins.Name) + "Accounts struct {\n")
		for _, acc := range ins.Accounts {
			b.WriteString("\t" + toExport(acc.Name) + " solana.PublicKey\n")
		}
		b.WriteString("}\n\n")

		// AccountMeta builder
		b.WriteString("func (a " + toExport(ins.Name) + "Accounts) ToAccountMetas() []*solana.AccountMeta {\n")
		b.WriteString("\tmetas := make([]*solana.AccountMeta, 0, " + fmt.Sprint(len(ins.Accounts)) + ")\n")
		for _, acc := range ins.Accounts {
			pkExpr := "a." + toExport(acc.Name)
			if acc.Address != "" {
				pkExpr = "default" + toExport(ins.Name) + toExport(acc.Name) + "()"
				b.WriteString("var default" + toExport(ins.Name) + toExport(acc.Name) + " = func() solana.PublicKey {\n")
				b.WriteString("\treturn solana.MustPublicKeyFromBase58(\"" + acc.Address + "\")\n")
				b.WriteString("}\n\n")
			}
			signer := acc.Signer
			// PDAs 或常量地址不应为 signer
			if acc.PDA != nil {
				signer = false
			}
			if acc.Address != "" {
				signer = false
			}
			// solana.NewAccountMeta(pubkey, isWritable, isSigner)
			b.WriteString("\tmetas = append(metas, solana.NewAccountMeta(" + pkExpr + ", " + boolStr(acc.Writable) + ", " + boolStr(signer) + "))\n")
		}
		b.WriteString("\treturn metas\n")
		b.WriteString("}\n\n")

		// Instruction builder
		b.WriteString("func Build" + toExport(ins.Name) + "(accounts " + toExport(ins.Name) + "Accounts, args " + toExport(ins.Name) + "Args) (solana.Instruction, error) {\n")
		b.WriteString("\tbuf := bytes.NewBuffer(make([]byte, 0, 128))\n")
		b.WriteString("\tbuf.Write(" + toExport(ins.Name) + "Discriminator)\n")
		if len(ins.Args) > 0 {
			b.WriteString("\tif err := bin.NewBorshEncoder(buf).Encode(args); err != nil {\n\t\treturn nil, fmt.Errorf(\"encode args: %w\", err)\n\t}\n")
		}
		b.WriteString("\tdata := buf.Bytes()\n")
		b.WriteString("\treturn solana.NewInstruction(ProgramKey, accounts.ToAccountMetas(), data), nil\n")
		b.WriteString("}\n\n")

		// PDA helpers if any
		for _, acc := range ins.Accounts {
			if acc.PDA == nil || len(acc.PDA.Seeds) == 0 {
				continue
			}
			b.WriteString("func Derive" + toExport(ins.Name) + toExport(acc.Name) + "PDA(accounts " + toExport(ins.Name) + "Accounts, args " + toExport(ins.Name) + "Args) (solana.PublicKey, uint8, error) {\n")
			b.WriteString("\tseeds := make([][]byte, 0, " + fmt.Sprint(len(acc.PDA.Seeds)) + ")\n")
			for _, seed := range acc.PDA.Seeds {
				switch seed.Kind {
				case "const":
					b.WriteString("\tseeds = append(seeds, " + bytesLiteral(seed.Value) + ")\n")
				case "account":
					field := toExport(pathHead(seed.Path))
					b.WriteString("\tseeds = append(seeds, accounts." + field + "[:])\n")
				case "arg":
					argField := toExport(pathHead(seed.Path))
					// assume number fits u64
					b.WriteString("\t{\n\t\ttmp := make([]byte, 8)\n\t\tbinary.LittleEndian.PutUint64(tmp, uint64(args." + argField + "))\n\t\tseeds = append(seeds, tmp)\n\t}\n")
				}
			}
			prog := "ProgramKey"
			if acc.PDA.Program != nil {
				if len(acc.PDA.Program.Value) > 0 {
					prog = "solana.PublicKeyFromBytes(" + bytesLiteral(acc.PDA.Program.Value) + ")"
				} else if acc.PDA.Program.Kind == "account" && acc.PDA.Program.Path != "" {
					prog = "accounts." + toExport(pathHead(acc.PDA.Program.Path))
				}
			}
			b.WriteString("\treturn solana.FindProgramAddress(seeds, " + prog + ")\n")
			b.WriteString("}\n\n")
		}
	}
	return b.String()
}

func generateErrors(pkg string, doc idl) string {
	var b strings.Builder
	header(&b, pkg)
	b.WriteString("type ProgramError struct {\n\tCode uint32\n\tName string\n\tMsg  string\n}\n\n")
	b.WriteString("var Errors = map[uint32]ProgramError{\n")
	for _, e := range doc.Errors {
		b.WriteString(fmt.Sprintf("\t%d: {Code: %d, Name: \"%s\", Msg: \"%s\"},\n", e.Code, e.Code, e.Name, escape(e.Msg)))
	}
	b.WriteString("}\n\n")
	b.WriteString("func ErrorFromCode(code uint32) (ProgramError, bool) {\n\terr, ok := Errors[code]\n\treturn err, ok\n}\n")
	return b.String()
}

func header(b *strings.Builder, pkg string) {
	b.WriteString("// Code generated by internal/gen; DO NOT EDIT.\n")
	b.WriteString("// Generated at " + time.Now().UTC().Format(time.RFC3339) + "\n\n")
	b.WriteString("package " + pkg + "\n\n")
}

func parseTypeDesc(raw json.RawMessage) (*idlTypeDesc, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var desc idlTypeDesc
	if err := json.Unmarshal(raw, &desc); err != nil {
		return nil, nil
	}
	return &desc, nil
}

func typeFields(desc *idlTypeDesc) []idlTypeField {
	fields := make([]idlTypeField, 0, len(desc.Fields))
	for i, raw := range desc.Fields {
		var f idlTypeField
		if err := json.Unmarshal(raw, &f); err == nil && len(f.Type) > 0 {
			fields = append(fields, f)
			continue
		}
		// Tuple-like field without name
		fields = append(fields, idlTypeField{
			Name: fmt.Sprintf("Field%d", i),
			Type: raw,
		})
	}
	return fields
}

func parseType(raw json.RawMessage) typeRef {
	var prim string
	if err := json.Unmarshal(raw, &prim); err == nil {
		return typeRef{Kind: prim}
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err == nil {
		if v, ok := m["option"]; ok {
			elem := parseType(v)
			return typeRef{Kind: "option", Elem: &elem}
		}
		if v, ok := m["vec"]; ok {
			elem := parseType(v)
			return typeRef{Kind: "vec", Elem: &elem}
		}
		if v, ok := m["array"]; ok {
			var arr []json.RawMessage
			_ = json.Unmarshal(v, &arr)
			if len(arr) == 2 {
				elem := parseType(arr[0])
				var ln int
				_ = json.Unmarshal(arr[1], &ln)
				return typeRef{Kind: "array", Elem: &elem, Len: ln}
			}
		}
		if v, ok := m["defined"]; ok {
			var def struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(v, &def)
			return typeRef{Kind: "defined", Defined: def.Name}
		}
	}
	return typeRef{Kind: "unknown"}
}

func goType(t typeRef) string {
	switch t.Kind {
	case "bool":
		return "bool"
	case "string":
		return "string"
	case "u8":
		return "uint8"
	case "u16":
		return "uint16"
	case "u32":
		return "uint32"
	case "u64":
		return "uint64"
	case "u128":
		return "bin.Uint128"
	case "i64":
		return "int64"
	case "i32":
		return "int32"
	case "pubkey":
		return "solana.PublicKey"
	case "option":
		return "*" + goType(*t.Elem)
	case "vec":
		return "[]" + goType(*t.Elem)
	case "array":
		return fmt.Sprintf("[%d]%s", t.Len, goType(*t.Elem))
	case "defined":
		return toExport(t.Defined)
	default:
		return "interface{}"
	}
}

func collectImports(t typeRef, set map[string]struct{}) {
	switch t.Kind {
	case "pubkey":
		set["solana"] = struct{}{}
	case "u128":
		set["bin"] = struct{}{}
	case "option":
		collectImports(*t.Elem, set)
	case "vec", "array":
		collectImports(*t.Elem, set)
	case "defined":
	default:
		if strings.HasPrefix(goType(t), "bin.") {
			set["bin"] = struct{}{}
		}
	}
}

func toExport(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func bytesLiteral(values []int) string {
	var buf bytes.Buffer
	buf.WriteString("[]byte{")
	for i, v := range values {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(fmt.Sprintf("%d", v))
	}
	buf.WriteString("}")
	return buf.String()
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func escape(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func pathHead(p string) string {
	if idx := strings.Index(p, "."); idx >= 0 {
		return p[:idx]
	}
	return p
}

func fail(formatStr string, args ...interface{}) {
	msg := fmt.Sprintf(formatStr, args...)
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
