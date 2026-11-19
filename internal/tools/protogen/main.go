// Command protogen compiles api/proto/plugin/v1/plugin.proto into Go protobuf
// and gRPC stubs without requiring an external C++ protoc binary.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	protocGenGoPkg     = "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6"
	protocGenGoGRPCPkg = "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1"
)

func main() {
	check := flag.Bool("check", false, "verify generated proto files are up to date without writing")
	flag.Parse()

	root, err := findRepoRoot()
	if err != nil {
		fatalf("locate repo root: %v", err)
	}
	protoDir := filepath.Join(root, "api", "proto", "plugin", "v1")

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{protoDir},
		}),
	}
	files, err := compiler.Compile(context.Background(), "plugin.proto")
	if err != nil {
		fatalf("compile plugin.proto: %v", err)
	}
	fdps := make([]*descriptorpb.FileDescriptorProto, 0, len(files))
	for _, f := range files {
		fdps = append(fdps, protodesc.ToFileDescriptorProto(f))
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"plugin.proto"},
		Parameter:      proto.String("paths=source_relative"),
		ProtoFile:      fdps,
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		fatalf("marshal CodeGeneratorRequest: %v", err)
	}

	generated := map[string][]byte{}
	for _, pluginPkg := range []string{protocGenGoPkg, protocGenGoGRPCPkg} {
		resp, err := runCodegenPlugin(root, pluginPkg, reqBytes)
		if err != nil {
			fatalf("run %s: %v", pluginPkg, err)
		}
		if resp.GetError() != "" {
			fatalf("%s reported error: %s", pluginPkg, resp.GetError())
		}
		for _, f := range resp.GetFile() {
			name := filepath.Base(f.GetName())
			generated[name] = []byte(f.GetContent())
		}
	}

	if *check {
		for name, want := range generated {
			dst := filepath.Join(protoDir, name)
			got, err := os.ReadFile(dst)
			if err != nil {
				fatalf("read %s: %v (run `go generate ./api/proto/plugin/v1/...`)", dst, err)
			}
			if !bytes.Equal(normalizeLF(got), normalizeLF(want)) {
				fatalf("%s is out of date (run `go generate ./api/proto/plugin/v1/...`)", dst)
			}
		}
		return
	}

	for name, content := range generated {
		dst := filepath.Join(protoDir, name)
		if err := os.WriteFile(dst, normalizeLF(content), 0o644); err != nil {
			fatalf("write %s: %v", dst, err)
		}
	}
}

func runCodegenPlugin(dir, pkg string, reqBytes []byte) (*pluginpb.CodeGeneratorResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", pkg)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	var resp pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func normalizeLF(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "protogen: "+format+"\n", args...)
	os.Exit(1)
}
