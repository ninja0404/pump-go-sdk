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
	Optional  bool     `json:"optional"`
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
	Kind     string            `json:"kind"`
	Fields   []json.RawMessage `json:"fields"`
	Variants []idlEnumVariant  `json:"variants"`
}

type idlEnumVariant struct {
	Name   string            `json:"name"`
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
		desc := mustParseTypeDesc(t.Name, t.Type)
		switch desc.Kind {
		case "struct":
			for _, f := range typeFields(desc) {
				collectImports(parseType(f.Type), imports)
			}
		case "enum":
			for _, variant := range desc.Variants {
				if len(variant.Fields) != 0 {
					panic(fmt.Sprintf("enum %s variant %s has unsupported fields", t.Name, variant.Name))
				}
			}
		default:
			panic(fmt.Sprintf("type %s has unsupported kind %q", t.Name, desc.Kind))
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
		desc := mustParseTypeDesc(t.Name, t.Type)
		switch desc.Kind {
		case "struct":
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
		case "enum":
			name := toExport(t.Name)
			b.WriteString("type " + name + " uint8\n\n")
			b.WriteString("const (\n")
			for i, variant := range desc.Variants {
				b.WriteString("\t" + name + toExport(variant.Name))
				if i == 0 {
					b.WriteString(" " + name + " = iota")
				}
				b.WriteString("\n")
			}
			b.WriteString(")\n\n")
		}
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

		if !typeNames[acc.Name] {
			panic(fmt.Sprintf("account %s has no matching type definition", acc.Name))
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

	needsBinarySeedEncoding := false
	for _, ins := range doc.Instructions {
		for _, acc := range ins.Accounts {
			if acc.PDA == nil {
				continue
			}
			for _, seed := range acc.PDA.Seeds {
				if seed.Kind == "arg" {
					needsBinarySeedEncoding = needsBinarySeedEncoding || seedTypeNeedsBinary(instructionArgType(ins, pathHead(seed.Path)))
				}
				if seed.Kind == "account" && strings.Contains(seed.Path, ".") {
					needsBinarySeedEncoding = needsBinarySeedEncoding || seedTypeNeedsBinary(nestedAccountFieldType(doc, seed.Path))
				}
			}
		}
	}

	b.WriteString("import (\n")
	b.WriteString("\t\"bytes\"\n")
	if needsBinarySeedEncoding {
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
		b.WriteString("\tRemainingAccounts []*solana.AccountMeta\n")
		b.WriteString("}\n\n")

		// AccountMeta builder
		b.WriteString("func (a " + toExport(ins.Name) + "Accounts) ToAccountMetas() []*solana.AccountMeta {\n")
		b.WriteString("\tmetas := make([]*solana.AccountMeta, 0, " + fmt.Sprint(len(ins.Accounts)) + "+len(a.RemainingAccounts))\n")
		for _, acc := range ins.Accounts {
			pkExpr := "a." + toExport(acc.Name)
			signer := acc.Signer
			if acc.PDA != nil || acc.Address != "" {
				signer = false
			}
			if acc.Optional {
				b.WriteString("\tif " + pkExpr + ".IsZero() {\n")
				b.WriteString("\t\tmetas = append(metas, solana.NewAccountMeta(ProgramKey, false, false))\n")
				b.WriteString("\t} else {\n")
				b.WriteString("\t\tmetas = append(metas, solana.NewAccountMeta(" + pkExpr + ", " + boolStr(acc.Writable) + ", " + boolStr(signer) + "))\n")
				b.WriteString("\t}\n")
				continue
			}
			if acc.Address != "" {
				pkExpr = "default" + toExport(ins.Name) + toExport(acc.Name) + "()"
				b.WriteString("var default" + toExport(ins.Name) + toExport(acc.Name) + " = func() solana.PublicKey {\n")
				b.WriteString("\treturn solana.MustPublicKeyFromBase58(\"" + acc.Address + "\")\n")
				b.WriteString("}\n\n")
			}
			// solana.NewAccountMeta(pubkey, isWritable, isSigner)
			b.WriteString("\tmetas = append(metas, solana.NewAccountMeta(" + pkExpr + ", " + boolStr(acc.Writable) + ", " + boolStr(signer) + "))\n")
		}
		b.WriteString("\tmetas = append(metas, a.RemainingAccounts...)\n")
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
			b.WriteString("func Derive" + toExport(ins.Name) + toExport(acc.Name) + "PDA(accounts " + toExport(ins.Name) + "Accounts, args " + toExport(ins.Name) + "Args")
			nestedParams := nestedAccountSeedParams(doc, acc.PDA.Seeds)
			for _, param := range nestedParams {
				b.WriteString(", " + param.Name + " " + goType(param.Type))
			}
			b.WriteString(") (solana.PublicKey, uint8, error) {\n")
			b.WriteString("\tseeds := make([][]byte, 0, " + fmt.Sprint(len(acc.PDA.Seeds)) + ")\n")
			for _, seed := range acc.PDA.Seeds {
				switch seed.Kind {
				case "const":
					b.WriteString("\tseeds = append(seeds, " + bytesLiteral(seed.Value) + ")\n")
				case "account":
					if strings.Contains(seed.Path, ".") {
						param := nestedAccountSeedParamForPath(nestedParams, seed.Path)
						b.WriteString(pdaTypedSeedCode(param.Name, param.Type))
					} else {
						field := toExport(pathHead(seed.Path))
						b.WriteString("\tseeds = append(seeds, accounts." + field + "[:])\n")
					}
				case "arg":
					argField := toExport(pathHead(seed.Path))
					argType := instructionArgType(ins, pathHead(seed.Path))
					b.WriteString(pdaArgSeedCode(argField, argType))
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
	b.WriteString("\n")
	b.WriteString("package " + pkg + "\n\n")
}

func parseTypeDesc(raw json.RawMessage) (*idlTypeDesc, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var desc idlTypeDesc
	if err := json.Unmarshal(raw, &desc); err != nil {
		return nil, err
	}
	return &desc, nil
}

func mustParseTypeDesc(name string, raw json.RawMessage) *idlTypeDesc {
	desc, err := parseTypeDesc(raw)
	if err != nil {
		panic(fmt.Sprintf("parse type %s: %v", name, err))
	}
	if desc == nil {
		panic(fmt.Sprintf("type %s has no descriptor", name))
	}
	return desc
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
	case "bytes":
		return "[]byte"
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
	case "i8":
		return "int8"
	case "i16":
		return "int16"
	case "i128":
		return "bin.Int128"
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
		panic(fmt.Sprintf("unsupported IDL type %q", t.Kind))
	}
}

func collectImports(t typeRef, set map[string]struct{}) {
	switch t.Kind {
	case "pubkey":
		set["solana"] = struct{}{}
	case "u128", "i128":
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

func instructionArgType(ins idlInstruction, name string) typeRef {
	for _, arg := range ins.Args {
		if arg.Name == name {
			return parseType(arg.Type)
		}
	}
	panic(fmt.Sprintf("instruction %s PDA seed references unknown arg %s", ins.Name, name))
}

func pdaArgSeedCode(field string, typ typeRef) string {
	return pdaTypedSeedCode("args."+field, typ)
}

func pdaTypedSeedCode(expression string, typ typeRef) string {
	switch typ.Kind {
	case "u8":
		return "\tseeds = append(seeds, []byte{byte(" + expression + ")})\n"
	case "u16", "i16":
		return "\t{\n\t\ttmp := make([]byte, 2)\n\t\tbinary.LittleEndian.PutUint16(tmp, uint16(" + expression + "))\n\t\tseeds = append(seeds, tmp)\n\t}\n"
	case "u32", "i32":
		return "\t{\n\t\ttmp := make([]byte, 4)\n\t\tbinary.LittleEndian.PutUint32(tmp, uint32(" + expression + "))\n\t\tseeds = append(seeds, tmp)\n\t}\n"
	case "u64", "i64":
		return "\t{\n\t\ttmp := make([]byte, 8)\n\t\tbinary.LittleEndian.PutUint64(tmp, uint64(" + expression + "))\n\t\tseeds = append(seeds, tmp)\n\t}\n"
	case "pubkey":
		return "\tseeds = append(seeds, " + expression + "[:])\n"
	case "string":
		return "\tseeds = append(seeds, []byte(" + expression + "))\n"
	case "bytes":
		return "\tseeds = append(seeds, " + expression + ")\n"
	default:
		panic(fmt.Sprintf("PDA seed expression %s has unsupported type %q", expression, typ.Kind))
	}
}

type nestedSeedParam struct {
	Path string
	Name string
	Type typeRef
}

func nestedAccountSeedParams(doc idl, seeds []idlSeed) []nestedSeedParam {
	params := make([]nestedSeedParam, 0)
	seen := make(map[string]struct{})
	for _, seed := range seeds {
		if seed.Kind != "account" || !strings.Contains(seed.Path, ".") {
			continue
		}
		if _, ok := seen[seed.Path]; ok {
			continue
		}
		seen[seed.Path] = struct{}{}
		parts := strings.Split(seed.Path, ".")
		name := lowerFirst(toExport(strings.Join(parts, "_")))
		params = append(params, nestedSeedParam{
			Path: seed.Path,
			Name: name,
			Type: nestedAccountFieldType(doc, seed.Path),
		})
	}
	return params
}

func nestedAccountSeedParamForPath(params []nestedSeedParam, path string) nestedSeedParam {
	for _, param := range params {
		if param.Path == path {
			return param
		}
	}
	panic(fmt.Sprintf("missing nested account seed parameter for %s", path))
}

func nestedAccountFieldType(doc idl, path string) typeRef {
	parts := strings.Split(path, ".")
	if len(parts) != 2 {
		panic(fmt.Sprintf("unsupported nested account seed path %q", path))
	}
	typeName := toExport(parts[0])
	for _, definition := range doc.Types {
		if toExport(definition.Name) != typeName {
			continue
		}
		desc := mustParseTypeDesc(definition.Name, definition.Type)
		if desc.Kind != "struct" {
			break
		}
		for _, field := range typeFields(desc) {
			if field.Name == parts[1] {
				return parseType(field.Type)
			}
		}
		break
	}
	panic(fmt.Sprintf("cannot resolve nested account seed type for %s", path))
}

func seedTypeNeedsBinary(typ typeRef) bool {
	switch typ.Kind {
	case "u16", "i16", "u32", "i32", "u64", "i64":
		return true
	default:
		return false
	}
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
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
