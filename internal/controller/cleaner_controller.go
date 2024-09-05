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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CleanerReconciler reconciles a Cleaner object
type CleanerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const openreplayFinalizer = "openreplay.com/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableMemcached represents the status of the Deployment reconciliation
	typeAvailableMemcached = "Available"
	// typeDegradedMemcached represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedMemcached = "Degraded"
)

// finalizeMemcached will perform the required operations before delete the CR.
func (r *CleanerReconciler) doFinalizerOperationsForMemcached(cr *argocd.Application) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/
	fmt.Println("Successfully deleted Memcached")

	apps := &argocd.ApplicationList{}
	if err := r.Client.List(context.TODO(), apps, client.MatchingLabels{"domain": "rjsh.com"}); err != nil {
		r.Recorder.Event(cr, "Warning", "ListError",
			fmt.Sprintf("Failed to list Argo Applications: %v", err))
		return
	}

	// Remove finalizers from each application
	for _, app := range apps.Items {
		fmt.Println(app.Name)
		if len(app.GetFinalizers()) > 0 {
			app.SetFinalizers([]string{})
			if err := r.Client.Update(context.TODO(), &app); err != nil {
				fmt.Printf("Failed to remove finalizer from Argo Application %s: %v", app.Name, err)
				continue
			}
		}
	}

	// List Argo ApplicationSets with label domain=rjsh.com
	appSets := &argocd.ApplicationSetList{}
	if err := r.Client.List(context.TODO(), appSets, client.MatchingLabels{"domain": "rjsh.com"}); err != nil {
		fmt.Printf("Failed to list Argo ApplicationSets: %v", err)
		return
	}

	// Remove finalizers from each ApplicationSet
	for _, appSet := range appSets.Items {
		if len(appSet.GetFinalizers()) > 0 {
			appSet.SetFinalizers([]string{})
			if err := r.Client.Update(context.TODO(), &appSet); err != nil {
				fmt.Printf("Failed to remove finalizer from Argo ApplicationSet %s: %v", appSet.Name, err)
				continue
			}
		}
	}

	// // The following implementation will raise an event
	// r.Recorder.Event(cr, "Warning", "Deleting",
	// 	fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
	// 		cr.Name,
	// 		cr.Namespace))
}

//+kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argoproj.io,resources=applications/finalizers,verbs=update

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
	log.Info("where are you")
	argocdApp := &argocd.Application{}
	err := r.Get(ctx, req.NamespacedName, argocdApp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("argocd resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get argocd")
		return ctrl.Result{}, err
	}

	isMemcachedMarkedToBeDeleted := argocdApp.GetDeletionTimestamp() != nil
	// TODO(user): your logic here
	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(argocdApp, openreplayFinalizer) && !isMemcachedMarkedToBeDeleted {
		log.Info("Adding Finalizer for Memcached")
		if ok := controllerutil.AddFinalizer(argocdApp, openreplayFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, argocdApp); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Memcached instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if isMemcachedMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(argocdApp, openreplayFinalizer) {
			log.Info("Performing Finalizer Operations for Memcached before delete CR")

			// if err := r.Status().Update(ctx, argocdApp); err != nil {
			// 	log.Error(err, "Failed to update Memcached status")
			// 	return ctrl.Result{}, err
			// }

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			// r.doFinalizerOperationsForMemcached(argocdApp)

			// TODO(user): If you add operations to the doFinalizerOperationsForMemcached method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the memcached Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, argocdApp); err != nil {
				log.Error(err, "Failed to re-fetch memcached")
				return ctrl.Result{}, err
			}

			// if err := r.Status().Update(ctx, argocdApp); err != nil {
			// 	log.Error(err, "Failed to update Memcached status")
			// 	return ctrl.Result{}, err
			// }
			//
			log.Info("Removing Finalizer for Memcached after successfully perform the operations")
			argocdApp.SetFinalizers([]string{})
			// if err := r.Client.Update(context.TODO(), argocdApp); err != nil {
			// 	fmt.Printf("Failed to remove finalizer from Argo ApplicationSet %s: %v", argocdApp.Name, err)
			// 	return ctrl.Result{Requeue: true}, nil
			// }

			if err := r.Update(ctx, argocdApp); err != nil {
				log.Error(err, "Failed to remove finalizer for Memcached")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CleanerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argocd.Application{}).
		Complete(r)
}
