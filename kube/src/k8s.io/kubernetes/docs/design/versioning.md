<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->

<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">

<h2>PLEASE NOTE: This document applies to the HEAD of the source tree</h2>

If you are using a released version of Kubernetes, you should
refer to the docs that go with that version.

<!-- TAG RELEASE_LINK, added by the munger automatically -->
<strong>
The latest release of this document can be found
[here](http://releases.k8s.io/release-1.1/docs/design/versioning.md).

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

# Kubernetes API and Release Versioning

Legend:

* **Kube X.Y.Z** refers to the version of Kubernetes that is released. This versions all components: apiserver, kubelet, kubectl, etc.  (**X** is the major version, **Y** is the minor version, and **Z** is the patch version.)
* **API vX[betaY]** refers to the version of the HTTP API.

## Release versioning

### Minor version scheme and timeline

* Kube X.Y.0-alpha.W, W > 0: Alpha releases are released roughly every two weeks directly from the master branch. No cherrypick releases. If there is a critical bugfix, a new release from master can be created ahead of schedule.
* Kube X.Y.Z-beta.W: When master is feature-complete for Kube X.Y, we will cut the release-X.Y branch 2 weeks prior to the desired X.Y.0 date and cherrypick only PRs essential to X.Y.  This cut will be marked as X.Y.0-beta.0, and master will be revved to X.Y+1.0-alpha.0.  If we're not satisfied with X.Y.0-beta.0, we'll release other beta releases, (X.Y.0-beta.W | W > 0) as necessary.
* Kube X.Y.0: Final release, cut from the release-X.Y branch cut two weeks prior.  X.Y.1-beta.0 will be tagged at the same commit on the same branch.  X.Y.0 occur 3 to 4 months after X.Y-1.0.
* Kube X.Y.Z, Z > 0: [Patch releases](#patch-releases) are released as we cherrypick commits into the release-X.Y branch, (which is at X.Y.Z-beta.W,) as needed.  X.Y.Z is cut straight from the release-X.Y branch, and X.Y.Z+1-beta.0 is tagged on the same commit.

### Major version timeline

There is no mandated timeline for major versions. They only occur when we need to start the clock on deprecating features. A given major version should be the latest major version for at least one year from its original release date.

### CI and dev version scheme

* Continuous integration versions also exist, and are versioned off of alpha and beta releases.  X.Y.Z-alpha.W.C+aaaa is C commits after X.Y.Z-alpha.W, with an additional +aaaa build suffix added; X.Y.Z-beta.W.C+bbbb is C commits after X.Y.Z-beta.W, with an additional +bbbb build suffix added.  Furthermore, builds that are built off of a dirty build tree, (during development, with things in the tree that are not checked it,) it will be appended with -dirty.

### Supported releases

We expect users to stay reasonably up-to-date with the versions of Kubernetes they use in production, but understand that it may take time to upgrade.

We expect users to be running approximately the latest patch release of a given minor release; we often include critical bug fixes in [patch releases](#patch-release), and so encourage users to upgrade as soon as possible.  Furthermore, we expect to "support" three minor releases at a time.  "Support" means we expect users to be running that version in production, though we may not port fixes back before the latest minor version.  For example, when v1.3 comes out, v1.0 will no longer be supported: basically, that means that the reasonable response to the question "my v1.0 cluster isn't working," is, "you should probably upgrade it, (and probably should have some time ago)".  With minor releases happening approximately every three months, that means a minor release is supported for approximately nine months.

This does *not* mean that we expect to introduce breaking changes between v1.0 and v1.3, but it does mean that we probably won't have reasonable confidence in clusters where some components are running at v1.0 and others running at v1.3.

This policy is in line with [GKE's supported upgrades policy](https://cloud.google.com/container-engine/docs/clusters/upgrade).

## API versioning

### Release versions as related to API versions

Here is an example major release cycle:

* **Kube 1.0 should have API v1 without v1beta\* API versions**
 * The last version of Kube before 1.0 (e.g. 0.14 or whatever it is) will have the stable v1 API. This enables you to migrate all your objects off of the beta API versions of the API and allows us to remove those beta API versions in Kube 1.0 with no effect. There will be tooling to help you detect and migrate any v1beta\* data versions or calls to v1 before you do the upgrade.
* **Kube 1.x may have API v2beta***
 * The first incarnation of a new (backwards-incompatible) API in HEAD is v2beta1. By default this will be unregistered in apiserver, so it can change freely. Once it is available by default in apiserver (which may not happen for several minor releases), it cannot change ever again because we serialize objects in versioned form, and we always need to be able to deserialize any objects that are saved in etcd, even between alpha versions. If further changes to v2beta1 need to be made, v2beta2 is created, and so on, in subsequent 1.x versions.
* **Kube 1.y (where y is the last version of the 1.x series) must have final API v2**
 * Before Kube 2.0 is cut, API v2 must be released in 1.x. This enables two things: (1) users can upgrade to API v2 when running Kube 1.x and then switch over to Kube 2.x transparently, and (2) in the Kube 2.0 release itself we can cleanup and remove all API v2beta\* versions because no one should have v2beta\* objects left in their database. As mentioned above, tooling will exist to make sure there are no calls or references to a given API version anywhere inside someone's kube installation before someone upgrades.
 * Kube 2.0 must include the v1 API, but Kube 3.0 must include the v2 API only. It *may* include the v1 API as well if the burden is not high - this will be determined on a per-major-version basis.

#### Rationale for API v2 being complete before v2.0's release

It may seem a bit strange to complete the v2 API before v2.0 is released, but *adding* a v2 API is not a breaking change. *Removing* the v2beta\* APIs *is* a breaking change, which is what necessitates the major version bump. There are other ways to do this, but having the major release be the fresh start of that release's API without the baggage of its beta versions seems most intuitive out of the available options.

## Patch releases

Patch releases are intended for critical bug fixes to the latest minor version, such as addressing security vulnerabilities, fixes to problems affecting a large number of users, severe problems with no workaround, and blockers for products based on Kubernetes.

They should not contain miscellaneous feature additions or improvements, and especially no incompatibilities should be introduced between patch versions of the same minor version (or even major version).

Dependencies, such as Docker or Etcd, should also not be changed unless absolutely necessary, and also just to fix critical bugs (so, at most patch version changes, not new major nor minor versions).

## Upgrades

* Users can upgrade from any Kube 1.x release to any other Kube 1.x release as a rolling upgrade across their cluster. (Rolling upgrade means being able to upgrade the master first, then one node at a time. See #4855 for details.)
  * However, we do not recommend upgrading more than two minor releases at a time (see [Supported releases](#supported-releases)), and do not recommend running non-latest patch releases of a given minor release.
* No hard breaking changes over version boundaries.
  * For example, if a user is at Kube 1.x, we may require them to upgrade to Kube 1.x+y before upgrading to Kube 2.x. In others words, an upgrade across major versions (e.g. Kube 1.x to Kube 2.x) should effectively be a no-op and as graceful as an upgrade from Kube 1.x to Kube 1.x+1. But you can require someone to go from 1.x to 1.x+y before they go to 2.x.

There is a separate question of how to track the capabilities of a kubelet to facilitate rolling upgrades. That is not addressed here.


<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/docs/design/versioning.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->
