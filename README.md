Design Doc: Kubernetes-Native Distributed AI Job Scheduler
1. Executive Summary
This document proposes the design of a custom Kubernetes Operator and Controller to manage Distributed Deep Learning Workloads (e.g., PyTorch/TensorFlow training jobs).

The core objective is to implement Gang Scheduling (All-or-Nothing) semantics to prevent resource deadlocks common in multi-GPU training clusters. The system will define a Custom Resource Definition (CRD) named MLJob and provide a control loop to manage the lifecycle, resource reservation, and fault tolerance of these jobs.

<img width="1024" height="559" alt="image" src="https://github.com/user-attachments/assets/478418f8-d378-40a5-ba8c-e2273bf1ce58" />

2. Problem Statement
In standard Kubernetes, the default scheduler schedules Pods individually. This creates a critical issue for distributed training workloads (e.g., MPI-based jobs) that require synchronous startup:

The Deadlock Scenario:

Cluster Capacity: 4 GPUs.

Job A requires 4 GPUs. Job B requires 4 GPUs.

Standard Scheduler starts 2 pods for Job A and 2 pods for Job B.

Result: Both jobs hang indefinitely waiting for resources that are held by the other. This causes 0% cluster utilization and operational deadlock.

Lack of Domain-Specific Fault Tolerance:

If one worker in an MPI ring fails, the entire training epoch fails. Standard K8s ReplicaSets simply restart the single pod, which often leads to state inconsistencies in the training framework.
<img width="2816" height="1536" alt="Gemini_Generated_Image_cn4e6bcn4e6bcn4e" src="https://github.com/user-attachments/assets/1764f737-349b-4ce9-8fed-6103b6e9ac9c" />

3. Goals & Non-Goals
3.1 Goals
Implement MLJob CRD: An abstraction for users to define distributed training parameters (workers, resources, images).

Gang Scheduling: Ensure that resources are reserved atomically. Either all required pods for a job are scheduled, or none are (the job remains Pending).

Atomic Lifecycle Management: If one pod in the group fails (e.g., Spot Instance eviction), the controller must terminate the remaining pods to reset the training state.

Observability: Expose Prometheus metrics for queue depth, scheduling latency, and resource wait time.
<img width="2816" height="1536" alt="Gemini_Generated_Image_cn4e6bcn4e6bcn4e (1)" src="https://github.com/user-attachments/assets/3818ef39-9d9c-422b-81fe-f6dfeb040aae" />

3.2 Non-Goals
Implementing the actual Machine Learning training code (we assume a generic dummy image).

Multi-cluster federation (Scope is limited to a single K8s cluster).

Hardware-level GPU sharing (MIG).

4. System Architecture
4.1 High-Level Diagram
4.2 The MLJob CRD
We define a struct that represents the desired state of the training job.

5. Detailed Design: The Scheduling Loop
The Controller will implement a Reconcile loop triggered by changes to MLJob or its owned Pods.

5.1 State Machine
The job moves through the following strict state transitions:

PENDING: Job submitted. Controller validates spec.

QUEUED (Virtual): Controller calculates cluster capacity.

Algorithm: ClusterTotal - (Sum(Requests) of Running Pods) >= JobReqs

If False: Stay in PENDING (Requeue with exponential backoff).

If True: Transition to SCHEDULING.

SCHEDULING (Atomic Action):

Create MinReplicas pods effectively simultaneously.

Inject PodGroup label to identify them.

Inject Affinity rules (optional) to ensure locality.

RUNNING: All pods are in Running state.

FAILED: If any SINGLE pod enters Failed state (and retries exceeded).

Action: Controller deletes ALL remaining pods to clean up.

COMPLETED: All pods exit with Code 0.

5.2 Gang Scheduling Implementation Logic
To avoid race conditions where two controllers might try to schedule concurrent jobs utilizing the same free space, we will use Optimistic Concurrency Control.

Snapshot: List all Nodes and list all non-terminated Pods in the cluster via Informer Cache (Low latency).

Calculation:

Execution: Create Pods.

5.3 Fault Tolerance (The "All-or-Nothing" Retry)
In Distributed Training (e.g., using torch.distributed), if Node 3 dies, Node 1, 2, and 4 will hang waiting for a handshake.

Controller Logic:

Watch Owns(&corev1.Pod{}).

If Pod.Status.Phase == Failed:

Check MLJob.Spec.RestartPolicy.

Action: Trigger DeleteCollection for all pods matching label job-name=<name>.

Reset MLJob.Status to PENDING to trigger a full reschedule (Simulating a Checkpoint Resume).
<img width="2816" height="1536" alt="Gemini_Generated_Image_cn4e6bcn4e6bcn4e (2)" src="https://github.com/user-attachments/assets/d5f02a06-f19d-4578-8cb5-7c3d0affd868" />

6. Observability & Metrics
To ensure production readiness (relevance to OCI), the operator will expose the following Prometheus metrics:

mljob_queue_depth: Number of jobs currently in Pending state.

mljob_scheduling_duration_seconds: Histogram of time from Submission to Running.

mljob_gpu_allocation_total: Total virtual GPUs currently reserved.

mljob_deadlock_prevented_count: Counter of how many times the scheduler held back a job due to insufficient atomic capacity.

7. Trade-offs & Alternatives
7.1 Custom Operator vs. K8s Scheduling Framework
Alternative: Write a plugin for the native K8s Scheduler (Scheduler Framework).

Decision: Build a Custom Operator.

Reasoning: Writing a Scheduler Plugin is complex and invasive for the cluster control plane. An Operator is portable, easier to deploy, and sufficient for implementing application-level Gang Scheduling via Admission Control logic.

7.2 Centralized vs. Distributed Locking
Current Design: Relies on the atomic nature of the K8s API Server. We do not use an external lock manager (like Redis/Zookeeper) to reduce architectural complexity.

Trade-off: Under extreme high concurrency (1000s of jobs/sec), we might encounter "Optimistic Locking Failures" requiring retries. This is acceptable for batch AI workloads where scheduling latency is less critical than execution throughput.

8. Future Work
Preemption: Implement priority queues where high-priority jobs can evict low-priority running jobs.

Bin Packing: Optimize node selection to fragment resources as little as possible.
