package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PVCOpts configures PersistentVolumeClaim generation.
type PVCOpts struct {
	Name             string
	Namespace        string
	Labels           map[string]string
	StorageSize      string
	StorageClassName string
	AccessModes      []corev1.PersistentVolumeAccessMode
}

// NewPVC builds a Kubernetes PersistentVolumeClaim.
func NewPVC(opts PVCOpts) *corev1.PersistentVolumeClaim {
	if opts.Labels == nil {
		opts.Labels = map[string]string{"app.kubernetes.io/managed-by": "wfctl"}
	}
	storageSize := opts.StorageSize
	if storageSize == "" {
		storageSize = "1Gi"
	}
	accessModes := opts.AccessModes
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{Name: opts.Name, Namespace: opts.Namespace, Labels: opts.Labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}
	if opts.StorageClassName != "" {
		pvc.Spec.StorageClassName = &opts.StorageClassName
	}
	return pvc
}
