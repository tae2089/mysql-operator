/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	galbimandudevcomv1alpha1 "github.com/tae2089/mysql-operator/api/v1alpha1"
)

// MysqlReconciler reconciles a Mysql object
type MysqlReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const secretDataKeyName = "ROOT_PASSWORD"
const containerName = "mysql"
const containerPort = 3306
const containerPasswordEnvName = "MYSQL_ROOT_PASSWORD"
const storageAmount = "10Gi"
const dataVolumeMountPath = "/var/lib/mysql"
const whenDeleted = "whenDeleted"
const whenScaled = "whenScaled"

// +kubebuilder:rbac:groups=galbimandudev.com,resources=mysqls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=galbimandudev.com,resources=mysqls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=galbimandudev.com,resources=mysqls/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/logs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Mysql object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MysqlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// TODO(user): your logic here
	reqMysql := &galbimandudevcomv1alpha1.Mysql{}
	err := r.Get(ctx, req.NamespacedName, reqMysql)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Println("Memcached resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}
	// Check if the resource alread exists, if not create it
	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: reqMysql.Name, Namespace: reqMysql.Namespace}, found)
	if err != nil && kerrors.IsNotFound(err) {

		if err = r.createSecret(ctx, reqMysql); err != nil {
			log.Println(err, "Faild to create Secret")
			return ctrl.Result{}, err
		}

		if err = r.createStatefulset(ctx, reqMysql); err != nil {
			log.Println(err, "Faild to create StatefulSet")
			return ctrl.Result{}, err
		}

		if err = r.createService(ctx, reqMysql); err != nil {
			log.Println(err, "Faild to create Service")
			return ctrl.Result{}, err
		}
		// Statefulset created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Println(err, "Faild to get StatefulSet")
		return ctrl.Result{}, err
	}
	// Ensure the statefulset size is the same as the desired statefulset size
	size := reqMysql.Spec.Replicas
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.Update(ctx, found)
		if err != nil {
			log.Println(err, "Faild to update StatefulSet")
			return ctrl.Result{}, err
		}
		// Spec updated - updated return and
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

func (r *MysqlReconciler) createSecret(ctx context.Context, reqMysql *galbimandudevcomv1alpha1.Mysql) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      reqMysql.Name,
			Namespace: reqMysql.Namespace,
		},
		Data: map[string][]byte{
			secretDataKeyName: []byte(reqMysql.Spec.RootPassword),
		},
		Type: corev1.SecretTypeOpaque,
	}
	controllerutil.SetControllerReference(reqMysql, secret, r.Scheme)
	err := r.Create(ctx, secret)
	log.Println("Creating a new Deployment")
	return err
}

func (r *MysqlReconciler) createStatefulset(ctx context.Context, reqMysql *galbimandudevcomv1alpha1.Mysql) error {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      reqMysql.Name,
			Namespace: reqMysql.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &reqMysql.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: getLabel(reqMysql.Name),
			},
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: getLabel(reqMysql.Name),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: "mysql:8.0.33",
							Ports: []corev1.ContainerPort{
								{
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: containerPort,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: containerPasswordEnvName,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key: secretDataKeyName,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: reqMysql.Name,
											},
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      reqMysql.Spec.DataPvcName,
									MountPath: dataVolumeMountPath,
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reqMysql.Spec.DataPvcName,
						Namespace: reqMysql.Namespace,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(storageAmount),
							},
						},
					},
				},
			},
		},
	}
	controllerutil.SetControllerReference(reqMysql, sts, r.Scheme)
	err := r.Create(ctx, sts)
	return err
}

func (r *MysqlReconciler) createService(ctx context.Context, reqMysql *galbimandudevcomv1alpha1.Mysql) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      reqMysql.Name,
			Namespace: reqMysql.Namespace,
			Labels:    getLabel(reqMysql.Name),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: getLabel(reqMysql.Name),
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     containerPort,
				},
			},
		},
	}
	controllerutil.SetControllerReference(reqMysql, svc, r.Scheme)
	err := r.Create(ctx, svc)
	return err
}

func (r *MysqlReconciler) isExists(ctx context.Context, reqMysql galbimandudevcomv1alpha1.Mysql) (bool, error) {
	namespace := reqMysql.Namespace
	name := reqMysql.Name
	sts := &appsv1.StatefulSet{}
	stsExists, err := r.checkResourceExists(ctx, namespace, name, sts)
	if err != nil {
		return false, err
	}
	secret := &corev1.Secret{}
	secretExists, err := r.checkResourceExists(ctx, namespace, name, secret)
	if err != nil {
		return false, err
	}
	svc := &corev1.Service{}
	svcExists, err := r.checkResourceExists(ctx, namespace, name, svc)
	if err != nil {
		return false, err
	}
	if !all([]bool{stsExists, secretExists, svcExists}) {
		return false, errors.New("Resource related to Mysql may not be deleted yet")
	}
	return stsExists && secretExists && svcExists, nil
}

func (r *MysqlReconciler) checkResourceExists(ctx context.Context, namespace string, name string, obj client.Object) (bool, error) {
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		if kerrors.IsNotFound(err) { // create
			return false, nil
		} else { // error
			return false, err
		}
	} else { // update
		return true, nil
	}
}

func all(target []bool) bool {
	initValue := target[0]
	for _, value := range target {
		if value != initValue {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *MysqlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&galbimandudevcomv1alpha1.Mysql{}).
		Complete(r)
}

func getLabel(name string) map[string]string {
	return map[string]string{
		"app": name,
	}
}
