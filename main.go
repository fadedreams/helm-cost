// Copyright 2026 fadedreams
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

// helm-cost: estimate the resource footprint and rough monthly cost of a chart
// before you install it. It renders the chart with `helm template`, then sums
// CPU/memory requests and limits across workloads (multiplied by replicas).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"text/tabwriter"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

type container struct {
	Name      string `json:"name"`
	Resources struct {
		Requests map[string]interface{} `json:"requests"`
		Limits   map[string]interface{} `json:"limits"`
	} `json:"resources"`
}

type podSpec struct {
	Containers []container `json:"containers"`
}

type workload struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int64 `json:"replicas"`
		Template struct {
			Spec podSpec `json:"spec"`
		} `json:"template"`
		Containers []container `json:"containers"` // for bare Pods
	} `json:"spec"`
}

type row struct {
	Kind, Name                     string
	Replicas                       int64
	CPUReq, CPULim, MemReq, MemLim int64 // per replica: millicores / bytes
}

func main() {
	priceCPU := flag.Float64("price-cpu", 0.03, "price per vCPU-hour (illustrative default, set your own)")
	priceMem := flag.Float64("price-mem", 0.004, "price per GiB-hour (illustrative default, set your own)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: helm cost [--price-cpu N] [--price-mem N] RELEASE CHART [helm template flags...]")
		fmt.Fprintln(os.Stderr, "Cost flags must come before RELEASE. Everything after is passed to `helm template`.")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	manifest, err := render(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	rows, notes := analyze(manifest)
	report(rows, notes, *priceCPU, *priceMem)
}

func render(args []string) ([]byte, error) {
	helm := os.Getenv("HELM_BIN")
	if helm == "" {
		helm = "helm"
	}
	cmd := exec.Command(helm, append([]string{"template"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template failed: %w\n%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func analyze(manifest []byte) ([]row, []string) {
	var rows []row
	var notes []string

	for _, doc := range bytes.Split(manifest, []byte("\n---")) {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var w workload
		if err := yaml.Unmarshal(doc, &w); err != nil {
			continue
		}

		var containers []container
		replicas := int64(1)

		switch w.Kind {
		case "Deployment", "StatefulSet", "ReplicaSet":
			containers = w.Spec.Template.Spec.Containers
			if w.Spec.Replicas != nil {
				replicas = *w.Spec.Replicas
			}
		case "DaemonSet":
			containers = w.Spec.Template.Spec.Containers
			notes = append(notes, fmt.Sprintf("DaemonSet/%s counted as 1 replica (real count = number of nodes)", w.Metadata.Name))
		case "Pod":
			containers = w.Spec.Containers
		case "Job", "CronJob":
			notes = append(notes, fmt.Sprintf("%s/%s skipped (not long-running)", w.Kind, w.Metadata.Name))
			continue
		default:
			continue
		}

		r := row{Kind: w.Kind, Name: w.Metadata.Name, Replicas: replicas}
		missing := false
		for _, c := range containers {
			cpuReq, cpuLim := quantity(c.Resources.Requests["cpu"]), quantity(c.Resources.Limits["cpu"])
			memReq, memLim := quantity(c.Resources.Requests["memory"]), quantity(c.Resources.Limits["memory"])
			r.CPUReq += cpuReq.MilliValue()
			r.CPULim += cpuLim.MilliValue()
			r.MemReq += memReq.Value()
			r.MemLim += memLim.Value()
			if cpuReq.IsZero() && memReq.IsZero() {
				missing = true
			}
		}
		if missing {
			notes = append(notes, fmt.Sprintf("%s/%s has container(s) with no requests set, so cost is underestimated", w.Kind, w.Metadata.Name))
		}
		rows = append(rows, r)
	}
	return rows, notes
}

func quantity(v interface{}) resource.Quantity {
	var s string
	switch t := v.(type) {
	case nil:
		return resource.Quantity{}
	case string:
		s = t
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	default:
		s = fmt.Sprint(t)
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

func report(rows []row, notes []string, priceCPU, priceMem float64) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tNAME\tREPLICAS\tCPU REQ\tCPU LIM\tMEM REQ\tMEM LIM")

	var cpuReq, cpuLim, memReq, memLim int64
	for _, r := range rows {
		n := r.Replicas
		fmt.Fprintf(tw, "%s\t%s\t%d\t%dm\t%dm\t%s\t%s\n",
			r.Kind, r.Name, n, r.CPUReq*n, r.CPULim*n, mi(r.MemReq*n), mi(r.MemLim*n))
		cpuReq += r.CPUReq * n
		cpuLim += r.CPULim * n
		memReq += r.MemReq * n
		memLim += r.MemLim * n
	}
	fmt.Fprintf(tw, "TOTAL\t\t\t%dm\t%dm\t%s\t%s\n", cpuReq, cpuLim, mi(memReq), mi(memLim))
	tw.Flush()

	const hoursPerMonth = 730.0
	cores := float64(cpuReq) / 1000.0
	gib := float64(memReq) / float64(1<<30)
	monthly := (cores*priceCPU + gib*priceMem) * hoursPerMonth

	fmt.Printf("\nEstimated monthly cost (based on requests): %.2f\n", monthly)
	fmt.Printf("  %.2f vCPU x %.4f/hr + %.2f GiB x %.4f/hr, x %.0f hours\n", cores, priceCPU, gib, priceMem, hoursPerMonth)
	fmt.Println("  Default prices are placeholders. Pass --price-cpu and --price-mem for your provider.")

	if len(notes) > 0 {
		fmt.Println("\nNotes:")
		for _, n := range notes {
			fmt.Println("  -", n)
		}
	}
}

func mi(n int64) string {
	return fmt.Sprintf("%dMi", n/(1024*1024))
}
