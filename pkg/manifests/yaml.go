package manifests

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"k8s.io/client-go/rest"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

type YamlFile struct {
	Name          string
	dynamicClient dynamic.Interface
	Resources     []unstructured.Unstructured
}

func NewYamlFile(path string, config *rest.Config) *YamlFile {
	client, _ := dynamic.NewForConfig(config)
	return &YamlFile{
		Name:          path,
		dynamicClient: client,
	}
}

func (f *YamlFile) Apply() error {
	if f.Resources == nil {
		f.Resources = parse(f.Name)
	}
	return create(f.Resources, f.dynamicClient)
}

// Parse parses a yaml file into a slice of unstructured resources
func parse(filename string) []unstructured.Unstructured {
	in, out := make(chan []byte, 10), make(chan unstructured.Unstructured, 10)
	go read(filename, in)
	go decode(in, out)
	result := []unstructured.Unstructured{}
	for spec := range out {
		result = append(result, spec)
	}
	return result
}

// Create applies unstructered resources
func create(resources []unstructured.Unstructured, dc dynamic.Interface) error {
	for _, spec := range resources {
		c, err := client(spec, dc)
		if err != nil {
			return err
		}
		_, err = c.Create(&spec, v1.CreateOptions{})
		if err != nil {
			fmt.Println("manifests::create ERROR :", spec.GetName(), err)
		}
	}
	return nil
}

// func decode(in chan []byte, out chan unstructured.Unstructured) {
// 	for buf := range in {
// 		spec := unstructured.Unstructured{}
// 		err := yaml.NewYAMLToJSONDecoder(bytes.NewReader(buf)).Decode(&spec)
// 		if err != nil {
// 			if err != io.EOF {
// 				fmt.Println("manifests::decode ERROR :", spec.GetName(), err)
// 			}
// 			continue
// 		}
// 		out <- spec
// 	}
// 	close(out)
// }

func decode(in chan []byte, out chan unstructured.Unstructured) {
	for buf := range in {
		spec := unstructured.Unstructured{}
		err := yaml.NewYAMLToJSONDecoder(bytes.NewReader(buf)).Decode(&spec)
		if err != nil {
			if err != io.EOF {
				fmt.Println("ERROR", spec.GetName(), err)
			}
			continue
		}
		out <- spec
	}
	close(out)
}

func buffer(file *os.File) []byte {
	var size int64 = bytes.MinRead
	if fi, err := file.Stat(); err == nil {
		size = fi.Size()
	}
	return make([]byte, size)
}

func read(filename string, sink chan []byte) {
	file, err := os.Open(filename)
	if err != nil {
		panic(err.Error())
	}

	manifests := yaml.NewDocumentDecoder(file)
	defer manifests.Close()
	buf := buffer(file)

	for {
		size, err := manifests.Read(buf)
		if err == io.EOF {
			break
		}
		b := make([]byte, size)
		copy(b, buf)
		sink <- b
	}
	close(sink)
}

func pluralize(kind string) string {
	ret := strings.ToLower(kind)
	switch {
	case strings.HasSuffix(ret, "s"):
		return fmt.Sprintf("%ses", ret)
	case strings.HasSuffix(ret, "policy"):
		return fmt.Sprintf("%sies", ret[:len(ret)-1])
	default:
		return fmt.Sprintf("%ss", ret)
	}
}

func client(spec unstructured.Unstructured, dc dynamic.Interface) (dynamic.ResourceInterface, error) {
	groupVersion, err := schema.ParseGroupVersion(spec.GetAPIVersion())
	if err != nil {
		return nil, err
	}
	groupVersionResource := groupVersion.WithResource(pluralize(spec.GetKind()))
	fmt.Println("manifests::client : groupVersionResource:", groupVersionResource)
	if ns := spec.GetNamespace(); ns == "" {
		return dc.Resource(groupVersionResource), nil
	} else {
		return dc.Resource(groupVersionResource).Namespace(ns), nil
	}
}
