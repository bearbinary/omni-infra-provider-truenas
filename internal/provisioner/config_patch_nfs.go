package provisioner

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildNFSStoragePatch generates a Talos machine config patch that deploys
// nfs-subdir-external-provisioner as a cluster inline manifest with a default StorageClass.
//
// This gives clusters persistent storage out of the box — PVCs bind immediately
// without any manual CSI driver setup.
func buildNFSStoragePatch(nfsServer, nfsPath string) ([]byte, error) {
	if nfsServer == "" {
		return nil, fmt.Errorf("NFS server address is required")
	}

	if nfsPath == "" {
		return nil, fmt.Errorf("NFS path is required")
	}

	manifests := buildNFSManifests(nfsServer, nfsPath)

	patch := map[string]any{
		"cluster": map[string]any{
			"inlineManifests": []map[string]any{
				{
					"name":     "nfs-storage",
					"contents": manifests,
				},
			},
		},
	}

	return json.Marshal(patch)
}

// buildNFSManifests returns the complete YAML for nfs-subdir-external-provisioner:
// ServiceAccount, ClusterRole, ClusterRoleBinding, Role, RoleBinding, Deployment, and StorageClass.
func buildNFSManifests(nfsServer, nfsPath string) string {
	const namespace = "nfs-provisioner"

	parts := []string{
		// Namespace
		fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s`, namespace),

		// ServiceAccount
		fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: nfs-provisioner
  namespace: %s`, namespace),

		// ClusterRole
		`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nfs-provisioner-runner
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["persistentvolumes"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "update", "patch"]`,

		// ClusterRoleBinding
		fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: nfs-provisioner-runner
subjects:
  - kind: ServiceAccount
    name: nfs-provisioner
    namespace: %s
roleRef:
  kind: ClusterRole
  name: nfs-provisioner-runner
  apiGroup: rbac.authorization.k8s.io`, namespace),

		// Role (leader election)
		fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: nfs-provisioner-leader
  namespace: %s
rules:
  - apiGroups: [""]
    resources: ["endpoints"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]`, namespace),

		// RoleBinding
		fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: nfs-provisioner-leader
  namespace: %s
subjects:
  - kind: ServiceAccount
    name: nfs-provisioner
    namespace: %s
roleRef:
  kind: Role
  name: nfs-provisioner-leader
  apiGroup: rbac.authorization.k8s.io`, namespace, namespace),

		// Deployment
		fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: nfs-provisioner
  namespace: %s
  labels:
    app: nfs-provisioner
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: nfs-provisioner
  template:
    metadata:
      labels:
        app: nfs-provisioner
    spec:
      serviceAccountName: nfs-provisioner
      containers:
        - name: nfs-provisioner
          image: registry.k8s.io/sig-storage/nfs-subdir-external-provisioner:v4.0.2
          volumeMounts:
            - name: nfs-root
              mountPath: /persistentvolumes
          env:
            - name: PROVISIONER_NAME
              value: truenas.io/nfs
            - name: NFS_SERVER
              value: "%s"
            - name: NFS_PATH
              value: "%s"
      volumes:
        - name: nfs-root
          nfs:
            server: "%s"
            path: "%s"`, namespace, nfsServer, nfsPath, nfsServer, nfsPath),

		// StorageClass (default)
		`apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: nfs
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
provisioner: truenas.io/nfs
parameters:
  archiveOnDelete: "false"
reclaimPolicy: Delete
allowVolumeExpansion: true
mountOptions:
  - nfsvers=4.1`,
	}

	return strings.Join(parts, "\n---\n")
}
