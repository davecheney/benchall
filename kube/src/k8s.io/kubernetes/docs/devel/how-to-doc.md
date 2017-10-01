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

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

# Document Conventions

Updated: 11/3/2015

*This document is oriented at users and developers who want to write documents for Kubernetes.*

**Table of Contents**
<!-- BEGIN MUNGE: GENERATED_TOC -->

- [Document Conventions](#document-conventions)
  - [General Concepts](#general-concepts)
  - [How to Get a Table of Contents](#how-to-get-a-table-of-contents)
  - [How to Write Links](#how-to-write-links)
  - [How to Include an Example](#how-to-include-an-example)
  - [Misc.](#misc)
    - [Code formatting](#code-formatting)
    - [Syntax Highlighting](#syntax-highlighting)
    - [Headings](#headings)
  - [What Are Mungers?](#what-are-mungers)
  - [Auto-added Mungers](#auto-added-mungers)
    - [Unversioned Warning](#unversioned-warning)
    - [Is Versioned](#is-versioned)
    - [Generate Analytics](#generate-analytics)

<!-- END MUNGE: GENERATED_TOC -->

## General Concepts

Each document needs to be munged to ensure its format is correct, links are valid, etc. To munge a document, simply run `hack/update-generated-docs.sh`. We verify that all documents have been munged using `hack/verify-generated-docs.sh`. The scripts for munging documents are called mungers, see the [mungers section](#what-are-mungers) below if you're curious about how mungers are implemented or if you want to write one.

## How to Get a Table of Contents

Instead of writing table of contents by hand, insert the following code in your md file:

```
<!-- BEGIN MUNGE: GENERATED_TOC -->
<!-- END MUNGE: GENERATED_TOC -->
```

After running `hack/update-generated-docs.sh`, you'll see a table of contents generated for you, layered based on the headings.

## How to Write Links

It's important to follow the rules when writing links. It helps us correctly versionize documents for each release.

Use inline links instead of urls at all times. When you add internal links to `docs/` or `examples/`, use relative links; otherwise, use `http://releases.k8s.io/HEAD/<path/to/link>`. For example, avoid using:

```
[GCE](https://github.com/kubernetes/kubernetes/blob/master/docs/getting-started-guides/gce.md)  # note that it's under docs/
[Kubernetes package](../../pkg/)                                                                # note that it's under pkg/
http://kubernetes.io/                                                                           # external link
```

Instead, use:

```
[GCE](../getting-started-guides/gce.md)                 # note that it's under docs/
[Kubernetes package](http://releases.k8s.io/HEAD/pkg/)  # note that it's under pkg/
[Kubernetes](http://kubernetes.io/)                     # external link
```

The above example generates the following links: [GCE](../getting-started-guides/gce.md), [Kubernetes package](http://releases.k8s.io/HEAD/pkg/), and [Kubernetes](http://kubernetes.io/).

## How to Include an Example

While writing examples, you may want to show the content of certain example files (e.g. [pod.yaml](../user-guide/pod.yaml)). In this case, insert the following code in the md file:

```
<!-- BEGIN MUNGE: EXAMPLE path/to/file -->
<!-- END MUNGE: EXAMPLE path/to/file -->
```

Note that you should replace `path/to/file` with the relative path to the example file. Then `hack/update-generated-docs.sh` will generate a code block with the content of the specified file, and a link to download it. This way, you save the time to do the copy-and-paste; what's better, the content won't become out-of-date every time you update the example file.

For example, the following:

```
<!-- BEGIN MUNGE: EXAMPLE ../user-guide/pod.yaml -->
<!-- END MUNGE: EXAMPLE ../user-guide/pod.yaml -->
```

generates the following after `hack/update-generated-docs.sh`:

<!-- BEGIN MUNGE: EXAMPLE ../user-guide/pod.yaml -->

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  labels:
    app: nginx
spec:
  containers:
  - name: nginx
    image: nginx
    ports:
    - containerPort: 80
```

[Download example](../user-guide/pod.yaml?raw=true)
<!-- END MUNGE: EXAMPLE ../user-guide/pod.yaml -->

## Misc.

### Code formatting

Wrap a span of code with single backticks (`` ` ``). To format multiple lines of code as its own code block, use triple backticks (```` ``` ````).

### Syntax Highlighting

Adding syntax highlighting to code blocks improves readability. To do so, in your fenced block, add an optional language identifier. Some useful identifier includes `yaml`, `console` (for console output), and `sh` (for shell quote format). Note that in a console output, put `$ ` at the beginning of each command and put nothing at the beginning of the output. Here's an example of console code block:

```
```console

$ kubectl create -f docs/user-guide/pod.yaml
pod "foo" created

```　
```

which renders as:

```console
$ kubectl create -f docs/user-guide/pod.yaml
pod "foo" created
```

### Headings

Add a single `#` before the document title to create a title heading, and add `##` to the next level of section title, and so on. Note that the number of `#` will determine the size of the heading.

## What Are Mungers?

Mungers are like gofmt for md docs which we use to format documents. To use it, simply place

```
<!-- BEGIN MUNGE: xxxx -->
<!-- END MUNGE: xxxx -->
```

in your md files. Note that xxxx is the placeholder for a specific munger. Appropriate content will be generated and inserted between two brackets after you run `hack/update-generated-docs.sh`. See [munger document](http://releases.k8s.io/HEAD/cmd/mungedocs/) for more details.

## Auto-added Mungers

After running `hack/update-generated-docs.sh`, you may see some code / mungers in your md file that are auto-added. You don't have to add them manually. It's recommended to just read this section as a reference instead of messing up with the following mungers.

### Unversioned Warning

UNVERSIONED_WARNING munger inserts unversioned warning which warns the users when they're reading the document from HEAD and informs them where to find the corresponding document for a specific release.

```
<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->
<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->
```

### Is Versioned

IS_VERSIONED munger inserts `IS_VERSIONED` tag in documents in each release, which stops UNVERSIONED_WARNING munger from inserting warning messages.

```
<!-- BEGIN MUNGE: IS_VERSIONED -->
<!-- TAG IS_VERSIONED -->
<!-- END MUNGE: IS_VERSIONED -->
```

### Generate Analytics

ANALYTICS munger inserts a Google Anaylytics link for this page.

```
<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
<!-- END MUNGE: GENERATED_ANALYTICS -->
```

<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/docs/devel/how-to-doc.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->
