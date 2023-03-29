// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"bytes"
	"testing"
)

func TestKindManifest(t *testing.T) {
	for _, tc := range []struct {
		values kindManifestValues
		out    string
	}{
		{
			values: kindManifestValues{
				Name:  "simple",
				Image: "alpine",
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "simple"
  name: "simple"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "simple"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "simple"
    spec:
      containers:
      - name: "simple"
        image: "alpine"
`,
		},
		{
			values: kindManifestValues{
				Name:    "command-and-args",
				Image:   "debian",
				Command: "bash",
				Args:    []string{"-c", "tail", "-f", "/dev/null"},
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args"
  name: "command-and-args"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "command-and-args"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "command-and-args"
    spec:
      containers:
      - name: "command-and-args"
        image: "debian"
        command:
        - "bash"
        args:
        - "-c"
        - "tail"
        - "-f"
        - "/dev/null"
`,
		},
		{
			values: kindManifestValues{
				Name:    "command-and-args-and-env-and-ports",
				Image:   "debian",
				Command: "bash",
				Args:    []string{"-c", "tail", "-f", "/dev/null"},
				Ports: map[string]int{
					"http":    8080,
					"metrics": 9090,
				},
				Envs: map[string]string{
					"FOO": "bar",
					"BAZ": "qux",
				},
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args-and-env-and-ports"
  name: "command-and-args-and-env-and-ports"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "command-and-args-and-env-and-ports"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "command-and-args-and-env-and-ports"
    spec:
      containers:
      - name: "command-and-args-and-env-and-ports"
        image: "debian"
        command:
        - "bash"
        args:
        - "-c"
        - "tail"
        - "-f"
        - "/dev/null"
        ports:
        - name: "http"
          containerPort: 8080
        - name: "metrics"
          containerPort: 9090
        env:
        - name: "BAZ"
          value: "qux"
        - name: "FOO"
          value: "bar"
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args-and-env-and-ports"
  name: "command-and-args-and-env-and-ports"
spec:
  type: NodePort
  selector:
    app.kubernetes.io/name: "command-and-args-and-env-and-ports"
  ports:
  - name: "http"
    port: 8080
  - name: "metrics"
    port: 9090
`,
		},
		{
			values: kindManifestValues{
				Name:    "command-and-args-and-resources-and-security-context",
				Image:   "debian",
				Command: "bash",
				Args:    []string{"-c", "tail", "-f", "/dev/null"},
				Bytes:   1024,
				CPUs:    2.5,
				Capabilities: []RunnableCapabilities{
					RunnableCapabilitiesSysAdmin,
				},
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args-and-resources-and-security-context"
  name: "command-and-args-and-resources-and-security-context"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "command-and-args-and-resources-and-security-context"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "command-and-args-and-resources-and-security-context"
    spec:
      containers:
      - name: "command-and-args-and-resources-and-security-context"
        image: "debian"
        command:
        - "bash"
        args:
        - "-c"
        - "tail"
        - "-f"
        - "/dev/null"
        resources:
          limits:
            memory: 1024
            cpu: 2.5
          requests:
            memory: 1024
            cpu: 2.5
        securityContext:
          capabilities:
            add:
            - SYS_ADMIN
`,
		},
		{
			values: kindManifestValues{
				Name:    "command-and-args-and-resources-and-security-context2",
				Image:   "debian",
				Command: "bash",
				Args:    []string{"-c", "tail", "-f", "/dev/null"},
				Bytes:   1024,
				CPUs:    2.5,
				Capabilities: []RunnableCapabilities{
					RunnableCapabilitiesSysAdmin,
				},
				Privileged: true,
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args-and-resources-and-security-context2"
  name: "command-and-args-and-resources-and-security-context2"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "command-and-args-and-resources-and-security-context2"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "command-and-args-and-resources-and-security-context2"
    spec:
      containers:
      - name: "command-and-args-and-resources-and-security-context2"
        image: "debian"
        command:
        - "bash"
        args:
        - "-c"
        - "tail"
        - "-f"
        - "/dev/null"
        resources:
          limits:
            memory: 1024
            cpu: 2.5
          requests:
            memory: 1024
            cpu: 2.5
        securityContext:
          privileged: true
          capabilities:
            add:
            - SYS_ADMIN
`,
		},
		{
			values: kindManifestValues{
				Name:    "command-and-args-and-resources-and-security-context3",
				Image:   "debian",
				Command: "bash",
				Args:    []string{"-c", "tail", "-f", "/dev/null"},
				Bytes:   1024,
				CPUs:    2.5,
				User:    "1000",
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "command-and-args-and-resources-and-security-context3"
  name: "command-and-args-and-resources-and-security-context3"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "command-and-args-and-resources-and-security-context3"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "command-and-args-and-resources-and-security-context3"
    spec:
      containers:
      - name: "command-and-args-and-resources-and-security-context3"
        image: "debian"
        command:
        - "bash"
        args:
        - "-c"
        - "tail"
        - "-f"
        - "/dev/null"
        resources:
          limits:
            memory: 1024
            cpu: 2.5
          requests:
            memory: 1024
            cpu: 2.5
        securityContext:
          runAsUser: 1000
`,
		},
		{
			values: kindManifestValues{
				Name:  "volumes",
				Image: "debian",
				Volumes: map[string]string{
					"foo": "/bar",
					"baz": "/qux",
				},
			},
			out: `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: "volumes"
  name: "volumes"
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: "volumes"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: "volumes"
    spec:
      containers:
      - name: "volumes"
        image: "debian"
        volumeMounts:
        - name: "baz"
          mountPath: /qux
        - name: "foo"
          mountPath: /bar
      volumes:
      - name: "baz"
        hostPath:
          path: /qux
      - name: "foo"
        hostPath:
          path: /bar
`,
		},
	} {
		t.Run(tc.values.Name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := kindManifest.Execute(&buf, tc.values); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tc.out {
				t.Errorf("got\n%s\nexpected\n%s", buf.String(), tc.out)
			}
		})
	}
}
