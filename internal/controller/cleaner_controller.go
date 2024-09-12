/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"

	argocd "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CleanerReconciler reconciles a Cleaner object
type CleanerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	openreplayFinalizer        = "openreplay.com/finalizer"
	openreplayClusterFinalizer = "openreplay.com/cluster"
)

// Definitions to manage status conditions
const (
	// typeAvailableMemcached represents the status of the Deployment reconciliation
	typeAvailableMemcached = "Available"
	// typeDegradedMemcached represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedMemcached = "Degraded"
)

func (r *CleanerReconciler) handleFinalizerOperations(ctx context.Context, obj client.Object) error {
	obj.SetFinalizers([]string{})
	if err := r.Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

//+kubebuilder:rbac:groups=corev1;argoproj.io,resources=secrets;applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=corev1;argoproj.io,resources=secrets/status;applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=corev1;argoproj.io,resources=secrets/finalizers;applications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cleaner object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *CleanerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	// argocdApp := &argocd.Application{}

	// Fetch the object
	objectType := "Unknown"
	isObjectMarkedToBeDeleted := false
	argoApp := &argocd.Application{}
	secret := &corev1.Secret{}
	var object client.Object

	// Attempt to fetch the object as a Secret
	if err := r.Get(ctx, req.NamespacedName, secret); err == nil {
		// If it's a Secret, set obj to secret and continue
		fmt.Println("Got Secret")
		objectType = "secret"
		object = secret
	} else {
		// Attempt to fetch the object as an ArgoCD Application
		if err := r.Get(ctx, req.NamespacedName, argoApp); err == nil {
			// If it's an Application, set obj to application and continue
			fmt.Println("Got ArgoApp")
			objectType = "application"
			object = argoApp
		} else {
			// If neither, log the error and return
			fmt.Println(err, "unable to fetch object")
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}
	// Determine the type of object and handle accordingly
	switch objectType {
	case "secret":
		// It's a Secret
		fmt.Printf("Secret created or updated: %s/%s\n", secret.Namespace, secret.Name)
		objectType = "Secret"
		isObjectMarkedToBeDeleted = secret.GetDeletionTimestamp() != nil
		fmt.Println(isObjectMarkedToBeDeleted, secret.Name)
		if controllerutil.ContainsFinalizer(secret, openreplayClusterFinalizer) && isObjectMarkedToBeDeleted {
			log.Info("Deleting")
			r.handleFinalizerOperations(ctx, object)
		}
	case "application":
		// It's an ArgoCD Application
		fmt.Printf("ArgoCD Application created or updated: %s/%s\n", argoApp.Namespace, argoApp.Name)
		objectType = "Application"
		isObjectMarkedToBeDeleted = argoApp.GetDeletionTimestamp() != nil
		if isObjectMarkedToBeDeleted {
			return ctrl.Result{}, r.handleFinalizerOperations(ctx, object)
		}
	default:
		log.Info("Unhandled object type")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CleanerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argocd.Application{}).
		Watches(
			&corev1.Secret{},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
