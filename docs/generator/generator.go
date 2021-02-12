package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camel "github.com/apache/camel-k/pkg/apis/camel/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func main() {
	if len(os.Args) != 3 {
		println("usage: generator kamelets-path doc-root")
		os.Exit(1)
	}

	dir := os.Args[1]
	out := os.Args[2]

	kamelets := listKamelets(dir)

	links := make([]string, 0)
	for _, k := range kamelets {
		img := saveImage(k, out)
		produceDoc(k, out, img)

		links = append(links, fmt.Sprintf("** xref:ROOT:%s.adoc[%s %s]", k.Name, img, k.Spec.Definition.Title))
	}

	saveNav(links, out)
}

func saveNav(links []string, out string) {
	content := "// THIS FILE IS AUTOMATICALLY GENERATED: DO NOT EDIT\n"
	content += "* xref:ROOT:index.adoc[Kamelet Catalog]\n"
	for _, l := range links {
		content += l + "\n"
	}
	content += "// THIS FILE IS AUTOMATICALLY GENERATED: DO NOT EDIT\n"
	dest := filepath.Join(out, "nav.adoc")
	if _, err := os.Stat(dest); err == nil {
		err = os.Remove(dest)
		handleGeneralError(fmt.Sprintf("cannot remove file %q", dest), err)
	}
	err := ioutil.WriteFile(dest, []byte(content), 0666)
	handleGeneralError(fmt.Sprintf("cannot write file %q", dest), err)
	fmt.Printf("%q written\n", dest)
}

func saveImage(k camel.Kamelet, out string) string {
	if ic, ok := k.ObjectMeta.Annotations["camel.apache.org/kamelet.icon"]; ok {
		svgb64Prefix := "data:image/svg+xml;base64,"
		if strings.HasPrefix(ic, svgb64Prefix) {
			data := ic[len(svgb64Prefix):]
			decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
			iconContent, err := ioutil.ReadAll(decoder)
			handleGeneralError(fmt.Sprintf("cannot decode icon from Kamelet %s", k.Name), err)
			dest := filepath.Join(out, "assets", "images", "kamelets", fmt.Sprintf("%s.svg", k.Name))
			if _, err := os.Stat(dest); err == nil {
				err = os.Remove(dest)
				handleGeneralError(fmt.Sprintf("cannot remove file %q", dest), err)
			}
			err = ioutil.WriteFile(dest, iconContent, 0666)
			handleGeneralError(fmt.Sprintf("cannot write file %q", dest), err)
			fmt.Printf("%q written\n", dest)
			return fmt.Sprintf("image:kamelets/%s.svg[]", k.Name)
		}
	}
	return ""
}

func produceDoc(k camel.Kamelet, out string, image string) {
	docFile := filepath.Join(out, "pages", k.Name + ".adoc")

	content := "// THIS FILE IS AUTOMATICALLY GENERATED: DO NOT EDIT\n"
	content += "= " + image + " " + k.Spec.Definition.Title + "\n"
	content += "\n"
	if prov, ok := k.Annotations["camel.apache.org/provider"]; ok {
		content += fmt.Sprintf("*Provided by: %q*\n", prov)
		content += "\n"
	}
	content += k.Spec.Definition.Description + "\n"
	content += "\n"
	content += "== Configuration Options\n"
	content += "\n"

	required := make(map[string]bool)
	keys := make([]string, 0, len(k.Spec.Definition.Properties))

	if len(k.Spec.Definition.Properties) > 0 {
		for _, r := range k.Spec.Definition.Required {
			required[r] = true
		}

		for key := range k.Spec.Definition.Properties {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			ri := required[keys[i]]
			rj := required[keys[j]]
			if ri && !rj {
				return true
			} else if !ri && rj {
				return false
			}
			return keys[i] < keys[j]
		})

		content += fmt.Sprintf("The following table summarizes the configuration options available for the `%s` Kamelet:\n", k.Name)

		content += `[width="100%",cols="2,^2,3,^2,^2,^3",options="header"]` + "\n"
		content += "|===\n"
		content += tableLine("Property", "Name", "Description", "Type", "Default", "Example")
		
		for _, key := range keys {
			prop := k.Spec.Definition.Properties[key]
			name := key
			if required[key] {
				name = "*" + name + " {empty}* *"
			}
			def := ""
			if prop.Default != nil {
				b, err := prop.Default.MarshalJSON()
				handleGeneralError(fmt.Sprintf("cannot marshal property %q default value in Kamelet %s", key, k.Name), err)
				def = "`" + strings.ReplaceAll(string(b), "`", "'") + "`"
			}
			ex := ""
			if prop.Example != nil {
				b, err := prop.Example.MarshalJSON()
				handleGeneralError(fmt.Sprintf("cannot marshal property %q example value in Kamelet %s", key, k.Name), err)
				ex = "`" + strings.ReplaceAll(string(b), "`", "'") + "`"
			}
			content += tableLine(name, prop.Title, prop.Description, prop.Type, def, ex)
		}

		content += "|===\n"
		content += "\n"
		content += "NOTE: Fields marked with ({empty}*) are mandatory.\n"

	} else {
		content += "The Kamelet does not specify any configuration option.\n"
	}

	content += "\n"
	content += "== Usage\n"
	content += "\n"
	content += fmt.Sprintf("This section summarizes how the `%s` can be used in various contexts.\n", k.Name)
	content += "\n"

	tp := k.ObjectMeta.Labels["camel.apache.org/kamelet.type"]
	if tp != "" {
		content += fmt.Sprintf("=== Knative %s\n", strings.Title(tp))
		content += "\n"

		content += fmt.Sprintf("The `%s` Kamelet can be used as Knative %s by binding it to a Knative object.\n", k.Name, tp)
		content += "\n"

		sampleConfig := make([]string, 0)
		for _, key := range keys {
			if !required[key] {
				continue
			}
			prop := k.Spec.Definition.Properties[key]
			if prop.Default == nil {
				ex := ""
				if prop.Example != nil {
					b, err := prop.Example.MarshalJSON()
					handleGeneralError(fmt.Sprintf("cannot marshal property %q example value in Kamelet %s", key, k.Name), err)
					ex = string(b)
				}
				if ex == "" {
					ex = `"The ` + prop.Title + `"`
				}
				sampleConfig = append(sampleConfig, fmt.Sprintf("%s: %s", key, ex))
			}
		}
		props := ""
		if len(sampleConfig) > 0 {
			props += "    properties:\n"
			for _, p := range sampleConfig {
				props += fmt.Sprintf("      %s\n", p)
			}
		}

		kameletRef := fmt.Sprintf(`    ref:
      kind: Kamelet
      apiVersion: camel.apache.org/v1alpha1
      name: %s
%s`, k.Name, props)

		knativeRef := `    ref:
      kind: InMemoryChannel
      apiVersion: messaging.knative.dev/v1
      name: mychannel
`

		sourceRef := kameletRef
		sinkRef := knativeRef
		if tp == "sink" {
			sourceRef = knativeRef
			sinkRef = kameletRef
		}

		binding := fmt.Sprintf(`apiVersion: camel.apache.org/v1alpha1
kind: KameletBinding
metadata:
  name: %s-binding
spec:
  source:
%s sink:
%s
`, k.Name, sourceRef, sinkRef)

		content += fmt.Sprintf(".%s-binding.yaml\n", k.Name)
		content += "[source,yaml]\n"
		content += "----\n"
		content += binding
		content += "----\n"

		content += "\n"
		content += "Make sure you have https://camel.apache.org/camel-k/latest/installation/installation.html[Camel K installed] into the Kubernetes cluster you're connected to.\n"
		content += "\n"
		content += fmt.Sprintf("Save the `%s-binding.yaml` file into your hard drive, then configure it according to your needs.\n", k.Name)
		content += "\n"
		content += fmt.Sprintf("You can run the %s using the following command:\n", tp)
		content += "\n"
		content += "[source,shell]\n"
		content += "----\n"
		content += fmt.Sprintf("kubectl apply -f %s-binding.yaml\n", k.Name)
		content += "----\n"

	}

	content += "// THIS FILE IS AUTOMATICALLY GENERATED: DO NOT EDIT\n"

	if _, err := os.Stat(docFile); err == nil {
		err = os.Remove(docFile)
		handleGeneralError(fmt.Sprintf("cannot remove file %q", docFile), err)
	}
	err := ioutil.WriteFile(docFile, []byte(content), 0666)
	handleGeneralError(fmt.Sprintf("cannot write to file %q", docFile), err)
	fmt.Printf("%q written\n", docFile)
}

func tableLine(val ...string) string {
	res := ""
	for _, s := range val {
		clean := strings.ReplaceAll(s, "|", "\\|")
		res += "| " + clean
	}
	return res + "\n"
}

func listKamelets(dir string) []camel.Kamelet {
	scheme := runtime.NewScheme()
	err := camel.AddToScheme(scheme)
	handleGeneralError("cannot to add camel APIs to scheme", err)

	codecs := serializer.NewCodecFactory(scheme)
	gv := camel.SchemeGroupVersion
	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    "Kamelet",
	}
	decoder := codecs.UniversalDecoder(gv)

	kamelets := make([]camel.Kamelet, 0)
	files, err := ioutil.ReadDir(dir)
	filesSorted := make([]string, 0)
	handleGeneralError(fmt.Sprintf("cannot list dir %q", dir), err)
	for _, fd := range files {
		if !fd.IsDir() && strings.HasSuffix(fd.Name(), ".kamelet.yaml") {
			fullName := filepath.Join(dir, fd.Name())
			filesSorted = append(filesSorted, fullName)
		}
	}
	sort.Strings(filesSorted)
	for _, fileName := range filesSorted {
		content, err := ioutil.ReadFile(fileName)
		handleGeneralError(fmt.Sprintf("cannot read file %q", fileName), err)

		json, err := yaml.ToJSON(content)
		handleGeneralError(fmt.Sprintf("cannot convert file %q to JSON", fileName), err)

		kamelet := camel.Kamelet{}
		_, _, err = decoder.Decode(json, &gvk, &kamelet)
		handleGeneralError(fmt.Sprintf("cannot unmarshal file %q into Kamelet", fileName), err)
		kamelets = append(kamelets, kamelet)
	}
	return kamelets
}

func handleGeneralError(desc string, err error) {
	if err != nil {
		fmt.Printf("%s: %+v\n", desc, err)
		os.Exit(2)
	}
}