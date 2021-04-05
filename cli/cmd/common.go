package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosuri/uitable"
	"github.com/pkg/errors"
	"github.com/raftAtGit/hl-fabric-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func debug(format string, v ...interface{}) {
	if verbose {
		fmt.Printf(format+"\n", v...)
	}
}

func info(format string, v ...interface{}) {
	fmt.Printf(format+"\n", v...)
}

// Prints the formatted message and calls os.Exit(1)
func fail(format string, v ...interface{}) {
	fmt.Printf(format+"\n", v...)
	os.Exit(1)
}

// EncodeTable is a helper function to decorate any error message with a bit
// more context and avoid writing the same code over and over for printers
func encodeTable(out io.Writer, table *uitable.Table) error {
	raw := table.Bytes()
	raw = append(raw, []byte("\n")...)
	_, err := out.Write(raw)
	if err != nil {
		return errors.Wrap(err, "unable to write table output")
	}
	return nil
}

func loadFabricNetwork(file string) (*v1alpha1.FabricNetwork, error) {
	debug("loading FabricNetwork from file %v", file)
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		fmt.Printf("failed to read file %v: %v \n", file, err)
		return nil, err
	}
	var network v1alpha1.FabricNetwork
	err = yaml.UnmarshalStrict(bytes, &network)
	if err != nil {
		fmt.Printf("failed to parse FabricNetwork from file %v: %v", file, err)
		return nil, err
	}
	debug("loaded and unmarshaled FabricNetwork from file %v", file)
	return &network, nil
}

func fabricNetworkExists(ctx context.Context, cl client.Client, namespace string, name string) (bool, *v1alpha1.FabricNetwork, error) {
	network := &v1alpha1.FabricNetwork{}
	err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, network)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return false, nil, nil
		}
		return false, nil, err
	}
	return true, network, nil
}

func secretExists(ctx context.Context, cl client.Client, namespace string, name string) (bool, error) {
	secret := &corev1.Secret{}
	err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func configMapExists(ctx context.Context, cl client.Client, namespace string, name string) (bool, error) {
	configMap := &corev1.ConfigMap{}
	err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, configMap)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TAR archives given file or folder
// modified from: https://gist.github.com/mimoo/25fc9716e0f1353791f5908f94d6e726
// TAR archives given file or folder
func compress(parentFolder string, childFolder string, buf io.Writer) error {
	// tar > gzip > buf
	zr := gzip.NewWriter(buf)
	defer zr.Close()

	tw := tar.NewWriter(zr)
	defer tw.Close()

	src := parentFolder + "/" + childFolder

	// walk through every file in the folder
	return filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		// generate tar header
		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		header.Name = strings.TrimPrefix(filepath.ToSlash(file), parentFolder+"/")
		if header.Name == "" {
			return nil
		}

		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// if not a dir, write file content
		if !fi.IsDir() {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			f.Close()
		}
		return nil
	})
}

// check for path traversal and correct forward slashes
func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}

func uncompress(src io.Reader, dst string) error {
	// ungzip
	zr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	// untar
	tr := tar.NewReader(zr)

	// uncompress each element
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}

		// validate name against path traversal
		if !validRelPath(header.Name) {
			return fmt.Errorf("tar contained invalid name: %v", header.Name)
		}

		// add dst + re-format slashes according to system
		target := filepath.Join(dst, header.Name)
		// if no join is needed, replace with ToSlash:
		// target = filepath.ToSlash(header.Name)

		// check the type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it (with 0755 permission)
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		// if it's a file create it (with same permission)
		case tar.TypeReg:
			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// copy over contents
			if _, err := io.Copy(fileToWrite, tr); err != nil {
				return err
			}
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			fileToWrite.Close()
		}
	}
	return nil
}

func createDirIfNotExists(dirPath string) error {
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		debug("output directory %v does not exist", outputDir)

		if err = os.MkdirAll(outputDir, os.ModePerm); err != nil {
			return err
		}
		debug("created output directory %v", outputDir)
	}
	return nil
}
