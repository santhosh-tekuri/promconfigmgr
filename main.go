// Copyright 2020 Santhosh Kumar Tekuri
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("usage: promconfigmgr /path/to/prometheus.yml /dir/to/generate")
		os.Exit(1)
	}

	b, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("loading %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}
	var prom map[string]interface{}
	if err := yaml.Unmarshal(b, &prom); err != nil {
		fmt.Printf("parsing %s: %s\n", os.Args[1], err)
		os.Exit(1)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	changed := make(chan struct{}, 1)
	notify := func(change string, obj interface{}) {
		if !prometheusConfig(obj) {
			return
		}
		configMap := obj.(*v1.ConfigMap)
		fmt.Printf("%6s: %s/%s\n", change, configMap.GetNamespace(), configMap.GetName())
		select {
		default:
		case changed <- struct{}{}:
		}
	}

	watch := cache.NewListWatchFromClient(cs.CoreV1().RESTClient(), "configmaps", "", fields.Everything())
	store, controller := cache.NewInformer(watch, &v1.ConfigMap{}, time.Second*0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			notify("add", obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			notify("update", newObj)
		},
		DeleteFunc: func(obj interface{}) {
			notify("delete", obj)
		},
	})
	go controller.Run(nil)

	// listen changes---
	t := time.NewTimer(time.Second * 5)
	tActive := true
	for {
		select {
		case <-changed:
			if !t.Stop() {
				if tActive {
					<-t.C
				}
			}
			t.Reset(time.Second * 5)
			tActive = true
		case <-t.C:
			tActive = false
			generate(prom, store, os.Args[2])
			reload()
		}
	}
}

func generate(prom map[string]interface{}, store cache.Store, dir string) {
	fmt.Println("generating configuration...")
	rulesDir := filepath.Join(dir, "rule_files")
	if err := os.RemoveAll(rulesDir); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		panic(err)
	}
	ruleFiles := make([]interface{}, 0)
	scrapeConfigs := make([]interface{}, 0)
	for _, obj := range store.List() {
		if !prometheusConfig(obj) {
			continue
		}
		cm := obj.(*v1.ConfigMap)
		fmt.Printf("including %s/%s\n", cm.GetNamespace(), cm.GetName())
		for k, v := range cm.Data {
			if k == "prometheus.yml" {
				var m map[string]interface{}
				if err := yaml.Unmarshal([]byte(v), &m); err != nil {
					fmt.Println(err)
				}
				if sc, ok := m["scrape_configs"].([]interface{}); ok {
					scrapeConfigs = append(scrapeConfigs, sc...)
				}
			} else {
				d := filepath.Join(rulesDir, cm.Namespace, cm.Name)
				if err := os.MkdirAll(d, 0700); err != nil {
					panic(err)
				}
				f := filepath.Join(d, k)
				if err := ioutil.WriteFile(f, []byte(v), 0700); err != nil {
					panic(err)
				}
				ruleFiles = append(ruleFiles, filepath.Join("rule_files", cm.Namespace, cm.Name, k))
			}
		}
	}
	prom["rule_files"] = ruleFiles
	prom["scrape_configs"] = scrapeConfigs
	b, err := yaml.Marshal(prom)
	if err != nil {
		panic(err)
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "prometheus.yml"), b, 0700); err != nil {
		panic(err)
	}
}

func prometheusConfig(obj interface{}) bool {
	cm := obj.(*v1.ConfigMap)
	for k, v := range cm.Annotations {
		if k == "prometheus.io/config" && v == "true" {
			return true
		}
	}
	return false
}

func reload() {
	fmt.Println("reloading configuration...")
	req, err := http.NewRequest("POST", "http://localhost:9090/-/reload", nil)
	if err != nil {
		panic(err)
	}
	for {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Println(err)
			time.Sleep(time.Second * 5)
			continue
		}
		fmt.Println(resp.Status)
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
}
