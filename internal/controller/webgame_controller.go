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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	webgamev1 "gitee.com/zjlll/game-operator/api/v1"
)

// WebGameReconciler reconciles a WebGame object
type WebGameReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=webgame.webgame.zjl,resources=webgames,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=webgame.webgame.zjl,resources=webgames/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=webgame.webgame.zjl,resources=webgames/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/finalizers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the WebGame object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *WebGameReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 	r *WebGameReconciler：这是接收者 WebGameReconciler，表示这是一个处理 WebGame 资源的控制器。
	// ctx context.Context：上下文对象，用于在函数调用之间传递信息，例如取消信号、超时等。
	// req ctrl.Request：包含触发本次 Reconcile 调用的请求信息。Request 通常包含了资源的名称空间和名称，标识了这个资源。
	// (ctrl.Result, error)：返回两个值，ctrl.Result 用来表示是否需要继续处理（如是否重试），error 用来报告是否发生了错误。

	logger := log.FromContext(ctx)
	logger.Info("webgame event received!")
	defer func() { logger.Info("webgame event handling completed") }()
	var webgame webgamev1.WebGame
	// var webgame webgamev1.WebGame：声明一个变量 webgame，它用于存储获取的 WebGame 实例。
	// r.Get(ctx, req.NamespacedName, &webgame)：通过 r.Get 从 Kubernetes 集群获取 WebGame 资源，req.NamespacedName 包含资源的名称和名称空间。获取结果会保存在 webgame 中，返回的错误（如果有）会被捕获。
	if err := r.Get(ctx, req.NamespacedName, &webgame); err != nil {
		// 获取 webgame 失败，并且错误原因是 "Not Found"
		// 说明触发本次 Reconcile 的是 webgame 的删除事件，不需要进一步处理，return 即可
		if errors.IsNotFound(err) {
			logger.Info("webgame not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// 获取 webgame 实例失败，但错误原因不是 "Not Found"，
		// 返回这个错误，触发本次 Reconcile 的事件会重新入队，等待重试，
		// 同时日志中会打印本次的错误
		logger.Error(err, "unable to fetch webgame, requeue")
		return ctrl.Result{}, err
	}
	selector := map[string]string{
		"gameType": webgame.Spec.GameType,
		"instance": webgame.GetName(),
	}
	var deployment = appsv1.Deployment{}
	deployment.SetNamespace(webgame.GetNamespace())
	// 这个getname其实获取的就是spec设置的name
	deployment.SetName(webgame.GetName())
	mutate := func() error {
		deployment.SetLabels(labels.Merge(deployment.GetLabels(), webgame.GetLabels()))
		deployment.Spec.Replicas = webgame.Spec.Replicas
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}
		deployment.Spec.Template.SetLabels(labels.Merge(webgame.GetLabels(), selector))

		container := corev1.Container{}
		if len(deployment.Spec.Template.Spec.Containers) != 0 {
			container = deployment.Spec.Template.Spec.Containers[0]
		}
		container.Name = webgame.GetName()
		container.Image = webgame.Spec.Image
		container.ImagePullPolicy = corev1.PullIfNotPresent
		container.Resources = corev1.ResourceRequirements{}
		container.Ports = []corev1.ContainerPort{{
			Name:          "web",
			ContainerPort: int32(webgame.Spec.ServerPort.IntValue()),
			Protocol:      corev1.ProtocolTCP,
		}}

		deployment.Spec.Template.Spec.Containers = []corev1.Container{container}
		return ctrl.SetControllerReference(&webgame, &deployment, r.Scheme)
	}
	res, err := ctrl.CreateOrUpdate(ctx, r.Client, &deployment, mutate)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != controllerutil.OperationResultNone {
		logger.Info("deployment changed", "res", res)
		return ctrl.Result{}, nil
	}
	// create service
	var service = corev1.Service{}

	service.SetNamespace(webgame.GetNamespace())
	service.SetName(webgame.GetName())
	mutate = func() error {
		service.SetLabels(labels.Merge(service.GetLabels(), webgame.GetLabels()))
		service.Spec.Selector = selector
		service.Spec.Type = corev1.ServiceTypeClusterIP
		service.Spec.Ports = []corev1.ServicePort{{
			Name:       "web",
			Port:       int32(webgame.Spec.ServerPort.IntValue()),
			TargetPort: webgame.Spec.ServerPort,
			Protocol:   corev1.ProtocolTCP,
		}}
		return controllerutil.SetControllerReference(&webgame, &service, r.Scheme)
	}
	res, err = ctrl.CreateOrUpdate(ctx, r.Client, &service, mutate)
	if err != nil {
		return ctrl.Result{}, err
	}

	if res != controllerutil.OperationResultNone {
		logger.Info("service changed", "res", res)
		return ctrl.Result{}, nil
	}
	// create ingress
	var (
		ingress  = networkingv1.Ingress{}
		pathType = networkingv1.PathTypePrefix
		// 构建路径
		path        = fmt.Sprintf("/%s/%s", selector["gameType"], selector["instance"])
		rewriteRule = fmt.Sprintf(`rewrite ^%s/(.*)$ /$1 break;`, path)
		annotations = map[string]string{
			"nginx.ingress.kubernetes.io/configuration-snippet": rewriteRule,
		}
	)

	ingress.SetNamespace(webgame.GetNamespace())
	ingress.SetName(webgame.GetName())

	mutate = func() error {
		ingress.SetLabels(labels.Merge(ingress.GetLabels(), webgame.GetLabels()))
		ingress.SetAnnotations(labels.Merge(ingress.GetAnnotations(), annotations))
		ingress.Spec = networkingv1.IngressSpec{
			IngressClassName: &webgame.Spec.IngressClass,
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							PathType: &pathType,
							Path:     path, // 使用动态生成的路径
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: service.GetName(),
									Port: networkingv1.ServiceBackendPort{
										Number: int32(webgame.Spec.ServerPort.IntValue()),
									},
								},
							},
						}},
					},
				},
			}},
		}

		// 设置 Controller 引用
		return controllerutil.SetControllerReference(&webgame, &ingress, r.Scheme)
	}
	logger.Info("Ingress configuration", "ingress", ingress)

	res, err = ctrl.CreateOrUpdate(ctx, r.Client, &ingress, mutate)
	if err != nil {
		return ctrl.Result{}, err
	}

	if res != controllerutil.OperationResultNone {
		logger.Info("ingress changed", "res", res)
		return ctrl.Result{}, nil
	}

	logger.Info("sync status")
	mutate = func() error {
		index := strings.TrimPrefix(webgame.Spec.IndexPage, "/")
		path := strings.TrimPrefix(path, "/")
		address := fmt.Sprintf("%s/%s/%s", webgame.Spec.Domain, path, index)
		webgame.Status.DeploymentStatus = *deployment.Status.DeepCopy()
		webgame.Status.GameAddress = address
		webgame.Status.ClusterIP = service.Spec.ClusterIP
		return nil
	}

	// update webgame status
	res, err = controllerutil.CreateOrPatch(ctx, r.Client, &webgame, mutate)
	if err != nil {
		return ctrl.Result{}, err
	}

	if res != controllerutil.OperationResultNone {
		// non-return
		logger.Info("webgame status synced")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebGameReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webgamev1.WebGame{}).
		// Named("webgame").
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
