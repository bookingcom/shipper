package application

import (
	"fmt"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName                   = "application-controller"
	DefaultRevisionHistoryLimit = 20
	MinRevisionHistoryLimit     = 1
	MaxRevisionHistoryLimit     = 1000
)

// Controller is a Kubernetes controller that creates Releases from
// Applications.
type Controller struct {
	shipperClientset clientset.Interface

	appLister listers.ApplicationLister
	appSynced cache.InformerSynced
	workqueue workqueue.RateLimitingInterface

	relLister listers.ReleaseLister
	relSynced cache.InformerSynced

	rbLister listers.RolloutBlockLister
	rbSynced cache.InformerSynced

	versionResolver shipperrepo.ChartVersionResolver

	recorder record.EventRecorder
}

// NewController returns a new Application controller.
func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	versionResolver shipperrepo.ChartVersionResolver,
	recorder record.EventRecorder,
) *Controller {
	appInformer := shipperInformerFactory.Shipper().V1alpha1().Applications()
	relInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()
	rbInformer := shipperInformerFactory.Shipper().V1alpha1().RolloutBlocks()

	c := &Controller{
		shipperClientset: shipperClientset,

		appLister: appInformer.Lister(),
		appSynced: appInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "application_controller_applications"),

		relLister: relInformer.Lister(),
		relSynced: relInformer.Informer().HasSynced,

		rbLister: rbInformer.Lister(),
		rbSynced: rbInformer.Informer().HasSynced,

		versionResolver: versionResolver,
		recorder:        recorder,
	}

	appInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueApp,
		UpdateFunc: func(_, new interface{}) {
			c.enqueueApp(new)
		},
		DeleteFunc: c.enqueueApp,
	})

	relInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueRel,
		UpdateFunc: func(old, new interface{}) {
			oldRel, oldOk := old.(*shipper.Release)
			newRel, newOk := new.(*shipper.Release)
			if oldOk && newOk && oldRel.ResourceVersion == newRel.ResourceVersion {
				klog.V(4).Info("Received Release re-sync Update")
				return
			}

			c.enqueueRel(newRel)
		},
		DeleteFunc: c.enqueueRel,
	})

	return c
}

// Run starts Application controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Application controller")
	defer klog.V(2).Info("Shutting down Application controller")

	if !cache.WaitForCacheSync(stopCh, c.appSynced, c.relSynced, c.rbSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the Application controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.applicationWorker, time.Second, stopCh)
	}

	klog.V(2).Info("Started Application controller")

	<-stopCh
}

func (c *Controller) applicationWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	defer c.workqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncApplication(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		c.workqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced Application %q", key)
	c.workqueue.Forget(obj)

	return true
}

func (c *Controller) enqueueRel(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	if n := len(rel.OwnerReferences); n != 1 {
		runtime.HandleError(fmt.Errorf("expected exactly one owner for Release %q but got %d", key, n))
		return
	}

	owner := rel.OwnerReferences[0]

	c.workqueue.Add(fmt.Sprintf("%s/%s", rel.Namespace, owner.Name))
}

func (c *Controller) enqueueApp(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

func (c *Controller) syncApplication(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.V(3).Infof("Application %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("Application")
	}

	app = app.DeepCopy()

	// Initialize annotations
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}

	// stable annotation trigger for de-duplication and sorting.
	// this annotation is always converted to an object in order to avoid code duplications and to
	// handle this annotation the same way across shipper.
	overrideRB, ok := app.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if ok {
		app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = rolloutblock.NewObjectNameList(overrideRB).String()
	}

	if app.Spec.RevisionHistoryLimit == nil {
		var i int32 = DefaultRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &i
	}

	// this would be better as OpenAPI validation, but it does not support
	// 'nullable' so it cannot be an optional field
	if *app.Spec.RevisionHistoryLimit < MinRevisionHistoryLimit {
		var min int32 = MinRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &min
	}

	if *app.Spec.RevisionHistoryLimit > MaxRevisionHistoryLimit {
		var max int32 = MaxRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &max
	}

	if err = c.processApplication(app); err != nil {
		if shippererrors.ShouldBroadcast(err) {
			c.recorder.Event(app,
				corev1.EventTypeWarning,
				"FailedApplication",
				err.Error())
		}
		return err
	}

	// TODO(asurikov): change to UpdateStatus when it's available.
	_, err = c.shipperClientset.ShipperV1alpha1().Applications(app.Namespace).Update(app)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(app, err).
			WithShipperKind("Application")
	}

	return nil
}

// wrapUpApplicationConditions fills conditions into the given shipper.Application
// object. It is meant to be called by processApplication() after a successful
// execution when other conditions have not been populated since no errors have
// happened.
//
// Its main responsibility is to fill the RollingOut condition based on the
// given Release slice, whether it is the first release being rolled out, a
// transition between two releases or, if stable and a release process is not
// ongoing, inform which Release is currently active.
func (c *Controller) wrapUpApplicationConditions(app *shipper.Application, rels []*shipper.Release) error {
	var (
		contenderRel *shipper.Release
		incumbentRel *shipper.Release
		err          error
	)

	// Required by GetContender() and GetIncumbent() below.
	rels = releaseutil.SortByGenerationDescending(rels)

	diff := diffutil.NewMultiDiff()
	defer func() {
		c.reportApplicationConditionChange(app, diff)
	}()

	abortingCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeAborting, corev1.ConditionFalse, "", "")
	diff.Append(apputil.SetApplicationCondition(&app.Status, *abortingCond))

	validHistoryCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeValidHistory, corev1.ConditionTrue, "", "")
	diff.Append(apputil.SetApplicationCondition(&app.Status, *validHistoryCond))

	releaseSyncedCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeReleaseSynced, corev1.ConditionTrue, "", "")
	diff.Append(apputil.SetApplicationCondition(&app.Status, *releaseSyncedCond))

	rollingOutCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRollingOut, corev1.ConditionUnknown, "", "")

	if contenderRel, err = apputil.GetContender(app.Name, rels); err != nil {
		// There's no contender release yet, so RollingOut condition is
		// Unknown, with error as message.
		rollingOutCond.Message = err.Error()
		goto End
	}

	if incumbentRel, err = apputil.GetIncumbent(app.Name, rels); err != nil && !shippererrors.IsIncumbentNotFoundError(err) {
		// Errors other than incumbent release not found bail out to not
		// report inconsistent status.
		rollingOutCond.Message = err.Error()
		goto End
	}

	if releaseutil.ReleaseComplete(contenderRel) {
		rollingOutCond.Status = corev1.ConditionFalse
		rollingOutCond.Message = fmt.Sprintf(ReleaseActiveMessageFormat, contenderRel.Name)
	} else if incumbentRel != nil {
		rollingOutCond.Status = corev1.ConditionTrue
		rollingOutCond.Message = fmt.Sprintf(TransitioningMessageFormat, incumbentRel.Name, contenderRel.Name)
	} else {
		rollingOutCond.Status = corev1.ConditionTrue
		rollingOutCond.Message = fmt.Sprintf(InitialReleaseMessageFormat, contenderRel.Name)
	}

End:
	diff.Append(apputil.SetApplicationCondition(&app.Status, *rollingOutCond))

	return nil
}

func (c *Controller) reportApplicationConditionChange(app *shipper.Application, diff diffutil.Diff) {
	if !diff.IsEmpty() {
		c.recorder.Event(app, corev1.EventTypeNormal, "ApplicationConditionChanged", diff.String())
	}
}

/*
* get all the releases owned by this application
* if 0, create new one (generation 0), return
* if >1, find latest (highest generation #), compare hash of that one to application template hash
* if same, do nothing
* if different, create new release (highest generation # + 1)
 */
func (c *Controller) processApplication(app *shipper.Application) error {
	var (
		appReleases     []*shipper.Release
		contender       *shipper.Release
		err             error
		generation      int
		highestObserved int
	)

	if appReleases, err = c.relLister.Releases(app.Namespace).ReleasesForApplication(app.Name); err != nil {
		return err
	}

	diff := diffutil.NewMultiDiff()
	defer func() {
		c.reportApplicationConditionChange(app, diff)
	}()

	// Required by subsequent calls to GetContender and GetIncumbent.
	appReleases = releaseutil.SortByGenerationDescending(appReleases)

	// Check if application chart spec is resolved: the original version
	// might contain either a specific version or a semver constraint.
	// If a semver constraint is found, it would be resolved in-place.
	if !apputil.ChartVersionResolved(app) {
		if _, err := apputil.ResolveChartVersion(app, c.versionResolver); err != nil {
			cond := apputil.NewApplicationCondition(
				shipper.ApplicationConditionTypeRollingOut,
				corev1.ConditionFalse,
				conditions.ChartVersionResolutionFailed,
				err.Error(),
			)

			diff.Append(apputil.SetApplicationCondition(&app.Status, *cond))

			if _, updErr := c.shipperClientset.ShipperV1alpha1().Applications(app.Namespace).Update(app); updErr != nil {
				return shippererrors.NewKubeclientUpdateError(app, updErr).WithShipperKind("Application")
			}
			return err
		}
	}

	rolloutBlocked, events, err := rolloutblock.BlocksRollout(c.rbLister, app)
	for _, ev := range events {
		c.recorder.Event(app, ev.Type, ev.Reason, ev.Message)
	}
	if rolloutBlocked {
		var msg string
		if err != nil {
			msg = err.Error()
		}

		condition := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeBlocked,
			corev1.ConditionTrue,
			shipper.RolloutBlockReason,
			msg,
		)
		diff.Append(apputil.SetApplicationCondition(&app.Status, *condition))

		return c.wrapUpApplicationConditions(app, appReleases)
	}

	condition := apputil.NewApplicationCondition(
		shipper.ApplicationConditionTypeBlocked,
		corev1.ConditionFalse,
		"",
		"",
	)
	diff.Append(apputil.SetApplicationCondition(&app.Status, *condition))

	if contender, err = apputil.GetContender(app.Name, appReleases); err != nil {
		// Anything else rather than not found err is an abort case
		if !shippererrors.IsContenderNotFoundError(err) {
			return err
		}

		// Contender doesn't exist, so we are covering the case where Shipper
		// is creating the first release for this application.
		if releaseName, iteration, err := c.releaseNameForApplication(app); err != nil {
			return err
		} else if rel, err := c.createReleaseForApplication(app, releaseName, iteration, generation); err != nil {
			releaseSyncedCond := apputil.NewApplicationCondition(
				shipper.ApplicationConditionTypeReleaseSynced,
				corev1.ConditionFalse,
				conditions.CreateReleaseFailed,
				fmt.Sprintf("could not create a new release: %q", err))
			diff.Append(apputil.SetApplicationCondition(&app.Status, *releaseSyncedCond))
			return err
		} else {
			appReleases = append(appReleases, rel)
		}
		// It seems that adding an object to the fakeClient doesn't
		// update listers and informers automatically during tests...
		// How should we do it then?
		apputil.SetHighestObservedGeneration(app, generation)

		app.Status.History = apputil.ReleasesToApplicationHistory(appReleases)
		if err = c.cleanUpReleasesForApplication(app, appReleases); err != nil {
			return err
		}

		return c.wrapUpApplicationConditions(app, appReleases)
	}

	if generation, err = releaseutil.GetGeneration(contender); err != nil {
		validHistoryCond := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenReleaseGeneration,
			err.Error())
		diff.Append(apputil.SetApplicationCondition(&app.Status, *validHistoryCond))
		return err
	}

	if highestObserved, err = apputil.GetHighestObservedGeneration(app); err != nil {
		validHistoryCond := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenApplicationObservedGeneration,
			err.Error())
		diff.Append(apputil.SetApplicationCondition(&app.Status, *validHistoryCond))
		return err
	}

	if generation < highestObserved {
		// the current contender's generation is lower than highest observed
		// generation. This usually means that a newer release has been
		// created and deleted. As side-effect of this, the contender's
		// environment will be copied back to the application.
		apputil.CopyEnvironment(app, contender)
		// keeping app annotations consistent with the new "old" release
		apputil.UpdateChartVersionResolvedAnnotation(app, contender.Spec.Environment.Chart.Version)
		apputil.SetHighestObservedGeneration(app, generation)

		abortingCond := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeAborting,
			corev1.ConditionTrue,
			"",
			fmt.Sprintf("abort in progress, returning state to release %q", contender.Name))
		diff.Append(apputil.SetApplicationCondition(&app.Status, *abortingCond))

		rollingOutCond := apputil.NewApplicationCondition(
			shipper.ApplicationConditionTypeRollingOut,
			corev1.ConditionTrue,
			"", "")
		diff.Append(apputil.SetApplicationCondition(&app.Status, *rollingOutCond))

		app.Status.History = apputil.ReleasesToApplicationHistory(appReleases)
		return c.cleanUpReleasesForApplication(app, appReleases)
	}

	if generation > highestObserved {
		// I think the best we can do is bump up to this new high water mark and
		// then proceed as normal if for some reason generation is higher than
		// highest observed. This should be possible in the case of a new release
		// with the higher generation is created but the application object
		// failed to update with the new highest observed generation.
		apputil.SetHighestObservedGeneration(app, generation)
		highestObserved = generation
	}

	if !identicalEnvironments(app.Spec.Template, contender.Spec.Environment) {
		// The application's template has been modified and is different than
		// the contender's environment. This means that a new release should
		// be created with the new template.
		highestObserved = highestObserved + 1
		if releaseName, iteration, err := c.releaseNameForApplication(app); err != nil {
			return err
		} else if rel, err := c.createReleaseForApplication(app, releaseName, iteration, highestObserved); err != nil {
			releaseSyncedCond := apputil.NewApplicationCondition(
				shipper.ApplicationConditionTypeReleaseSynced,
				corev1.ConditionFalse,
				conditions.CreateReleaseFailed,
				err.Error())
			diff.Append(apputil.SetApplicationCondition(&app.Status, *releaseSyncedCond))

			rollingOutCond := apputil.NewApplicationCondition(
				shipper.ApplicationConditionTypeRollingOut,
				corev1.ConditionFalse,
				conditions.CreateReleaseFailed,
				err.Error())
			diff.Append(apputil.SetApplicationCondition(&app.Status, *rollingOutCond))
			return err
		} else {
			appReleases = append(appReleases, rel)
		}
	}

	apputil.SetHighestObservedGeneration(app, highestObserved)

	app.Status.History = apputil.ReleasesToApplicationHistory(appReleases)
	if err = c.cleanUpReleasesForApplication(app, appReleases); err != nil {
		return err
	}

	return c.wrapUpApplicationConditions(app, appReleases)
}

func (c *Controller) cleanUpReleasesForApplication(app *shipper.Application, releases []*shipper.Release) error {
	var completedReleases []*shipper.Release

	// Process releases by a predictable, ascending generation order.
	releases = releaseutil.SortByGenerationAscending(releases)

	namespace := app.GetNamespace()
	releaseErrors := shippererrors.NewMultiError()

	// Delete any releases that are not completed. Don't touch the latest
	// release because a release that isn't complete and is the last
	// release just means that the user is rolling out the application.
	for i := 0; i < len(releases)-1; i++ {
		rel := releases[i]
		if releaseutil.ReleaseComplete(releases[i]) {
			completedReleases = append(completedReleases, releases[i])
			continue
		}

		err := c.shipperClientset.ShipperV1alpha1().Releases(namespace).Delete(rel.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// Skip this release: it's already deleted.
				continue
			}

			releaseErrors.Append(shippererrors.
				NewKubeclientDeleteError(namespace, rel.GetName(), err).
				WithShipperKind("Release"))
		}
	}

	// Delete the first X ordered by generation. Bail out on any error so that we
	// maintain the invariant that we always delete oldest first (rather than
	// failing to delete A and successfully deleting B and C in an 'A B C'
	// history).
	revisionHistoryLimit := math.Max(float64(*app.Spec.RevisionHistoryLimit), 1)
	overhead := len(completedReleases) - int(revisionHistoryLimit)
	for i := 0; i < overhead; i++ {
		rel := completedReleases[i]
		err := c.shipperClientset.ShipperV1alpha1().Releases(namespace).Delete(rel.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// Skip this release: it's already deleted.
				continue
			}

			releaseErrors.Append(shippererrors.
				NewKubeclientDeleteError(namespace, rel.GetName(), err).
				WithShipperKind("Release"))

			break
		}
	}

	return releaseErrors.Flatten()
}
