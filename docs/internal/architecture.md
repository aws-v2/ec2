# SageMaker Platform Architecture

## Overview

The SageMaker platform is a distributed workflow engine designed to execute arbitrary workloads on isolated compute resources. While inspired by AWS SageMaker, the platform is intentionally more general-purpose. Rather than focusing exclusively on machine learning, it allows users to execute any script—from data ingestion and preprocessing to model training, media processing, web scraping, and deployment.

The platform treats every workload as a sequence of independent execution units connected through artifacts. Each execution unit receives input data, performs work, produces output artifacts, and terminates. This design keeps workloads reproducible, isolated, and scalable while remaining flexible enough to support a wide range of use cases.

Unlike traditional workflow systems that tightly couple users to a predefined execution model, this platform only defines the execution environment. Users remain free to organize their code however they choose.

---

# Design Philosophy

The platform follows a small number of guiding principles that influence every architectural decision.

## 1. The Platform Does Not Understand User Code

The platform never attempts to interpret or classify a user's script.

A script may:

* Download videos from YouTube.
* Scrape product prices.
* Query a PostgreSQL database.
* Fine-tune a language model.
* Render Blender scenes.
* Generate reports.
* Convert videos.
* Train a regression model.

From the platform's perspective, every one of these workloads is simply "a program that should execute."

This separation keeps the execution engine generic. The orchestration layer concerns itself with compute, storage, networking, scheduling, and artifact management, leaving application-specific logic entirely to the user.

---

## 2. Compute Is Disposable

Virtual machines exist only to execute work.

Every workload executes inside a freshly provisioned environment. Once execution completes and artifacts have been collected, the virtual machine may be destroyed.

This approach provides several benefits:

* predictable execution environments
* reproducible workloads
* improved security
* simplified resource management
* automatic cleanup after execution

Users should never rely on state stored inside a VM after execution has completed.

---

## 3. Artifacts Are the Contract Between Tasks

Tasks do not communicate directly.

Instead, every task produces one or more artifacts which become inputs to downstream tasks.

Examples of artifacts include:

* datasets
* trained models
* checkpoints
* reports
* videos
* images
* log files

This model removes coupling between scripts and allows pipelines to evolve independently.

---

## 4. Scripts Own Their Dependencies

Dependencies belong to individual scripts rather than entire pipelines or nodes.

Each script executes inside an isolated runtime containing only the packages required by that script.

This avoids version conflicts and allows completely unrelated workloads to coexist within the same pipeline.

For example, one script may require:

```
requests==2.31
beautifulsoup4
```

while another requires:

```
requests==3.x
torch
transformers
```

Because each script executes in its own isolated container, both environments can coexist without conflict.

---

# High-Level Architecture

The platform is composed of several logical layers.

```
Pipeline
    │
    ▼
Node
    │
    ▼
Task
    │
    ▼
Container
    │
    ▼
Virtual Machine
    │
    ▼
Physical Host
```

Each layer has a single responsibility.

Pipelines organize work.

Nodes group related tasks.

Tasks execute user code.

Containers isolate dependencies.

Virtual machines provide compute.

Physical hosts provide infrastructure.

Keeping these responsibilities separate allows the platform to evolve without affecting user workloads.

---

# Pipelines

A pipeline is a directed workflow describing how work should be performed.

Typical pipelines may contain stages such as:

```
Ingest
    │
    ▼
Clean
    │
    ▼
Train
    │
    ▼
Evaluate
    │
    ▼
Deploy
```

These stage names are intentionally semantic.

The execution engine does not attach special meaning to the names.

For example, nothing prevents a user from:

* training a model inside an Ingest node
* downloading datasets inside a Deploy node
* combining the entire workflow into a single node

The labels exist purely to improve readability and organization.

---

# Nodes

A node represents a logical stage within a pipeline.

Nodes do not execute code directly.

Instead, each node contains one or more independent tasks.

For example:

```
Ingest

├── youtube.py
├── stocks.py
└── weather.py
```

Although these scripts perform different operations, they all contribute toward the same logical objective: collecting data.

Nodes also describe the compute resources required for execution.

For example:

* CPU cores
* Memory
* Storage
* GPU requirements
* Network access

The scheduler uses these requirements when selecting the appropriate virtual machine.

Importantly, a node describes *what resources are required*, not *where those resources come from*. Infrastructure decisions remain the responsibility of the scheduler.

---

# Tasks

Tasks are the smallest schedulable execution unit within the platform.

A task consists of:

* a script
* a dependency definition
* execution metadata
* runtime configuration

Unlike nodes, tasks actually execute code.

Every task executes independently inside its own isolated runtime.

This allows multiple tasks within the same node to use completely different dependency sets without interfering with one another.

---

# Why Tasks Execute Independently

Suppose an ingest node contains three scripts.

```
youtube.py
stocks.py
reddit.py
```

Each script requires different Python packages.

Running all three scripts inside a single Python environment would inevitably create dependency conflicts.

Instead, every task executes inside its own isolated container.

Each container installs only the dependencies required for that specific task.

Once execution completes, the container is destroyed.

This guarantees reproducible execution while eliminating dependency conflicts.

---

# Virtual Machines

Virtual machines provide the execution environment for tasks.

Rather than provisioning a separate VM for every script, the platform provisions a VM capable of executing the tasks assigned to a node.

The VM contains only a minimal runtime environment:

* Ubuntu
* Docker
* Cloud Agent
* Cached runtime images

The VM does not contain user code.

Instead, user code is copied into a temporary workspace before execution begins.

---

# Why Containers Execute Inside Virtual Machines

Containers solve dependency isolation.

Virtual machines solve infrastructure isolation.

These are different problems.

Virtual machines provide:

* security boundaries
* hardware isolation
* GPU allocation
* network isolation
* resource scheduling

Containers provide:

* dependency isolation
* reproducible environments
* lightweight execution
* fast startup
* filesystem isolation

Using both technologies allows the platform to benefit from the strengths of each.

---

# Workspace

Before execution begins, the Cloud Agent creates a temporary workspace for the job.

```
/workspace/<job-id>/

    code/

    input/

    output/

    logs/

    cache/
```

The workspace serves as the communication layer between the platform and the user's scripts.

User code is copied into the `code` directory.

Input artifacts are downloaded into `input`.

Generated artifacts are placed into `output`.

Logs produced during execution are streamed from `logs` back to the control plane.

After execution completes and artifacts have been uploaded, the workspace may be removed.

---

# The SDK

The SDK intentionally remains minimal.

Its purpose is not to replace existing Python libraries but to provide a small bridge between user code and the platform.

Currently, the SDK exposes two primary concepts:

* retrieving input artifacts
* publishing output artifacts

For example, a task may request an artifact produced by a previous task.

```
dataset = ctx.input("training-data")
```

The SDK automatically downloads the artifact into the workspace and returns the local path.

Similarly, publishing an artifact is as simple as:

```
ctx.publish(
    "trained-model",
    "./model"
)
```

The platform is then responsible for storing the artifact, recording metadata, and making it available to downstream tasks.

The user's code never interacts directly with object storage.

---

# Execution Lifecycle

Every task follows the same execution lifecycle.

1. The scheduler determines that a task is ready to execute.
2. A suitable virtual machine is provisioned.
3. The Cloud Agent creates a temporary workspace.
4. Input artifacts are downloaded.
5. A container is started.
6. Task dependencies are installed.
7. The user's script executes.
8. Output artifacts are collected.
9. Artifacts are uploaded to object storage.
10. Logs and metadata are recorded.
11. The container exits.
12. The virtual machine is returned to the scheduler or destroyed.

Because every task follows the same lifecycle, the platform can execute a wide variety of workloads without requiring task-specific logic.

---

# Conclusion

The core idea behind the platform is deliberately simple:

> Users write arbitrary code. The platform is responsible for everything else.

By separating user logic from infrastructure concerns, the platform provides a consistent execution model regardless of whether the workload involves machine learning, data engineering, media processing, automation, or any other computational task.

This separation keeps the system modular, scalable, and easy to extend while allowing users to focus entirely on solving their own problems rather than managing infrastructure.
