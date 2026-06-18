# exploreService Deployment & Operation Guide

This document describes how to deploy the `exploreService` gRPC microservice to a Kubernetes cluster (such as Amazon EKS) and connect it to an external Amazon Aurora PostgreSQL database.

## 📌 Prerequisites

Before running the deployment commands, ensure your local environment meets the following requirements:
1. **Toolchain**: `kubectl` and `aws-cli` must be installed locally and configured with access to your target EKS cluster.
2. **Network Connectivity**: The Security Group of your Amazon Aurora PostgreSQL instance must allow inbound traffic on port `5432` from the EKS nodes' IP range.
3. **Docker Image**: The Go binary must be built via the Dockerfile and pushed to your AWS ECR repository.

---

## 🛠️ Step 1: Initialize Configuration Files

To protect sensitive infrastructure endpoints and credentials, the active configuration files are ignored by Git. You must initialize them from the provided templates.

Navigate to the deployment directory and copy the `.example` files:

```bash
cd /deploy/k8s/
cp secret.yaml.example secret.yaml
cp deployment.yaml.example deployment.yaml
cp service.yaml.example service.yaml
```

### Configuration Updates Required:
* **`secret.yaml`**: Replace `mysecretpassword` with your actual Aurora PostgreSQL database password.
* **`deployment.yaml`**: 
  * Replace `<your_aws_account_id>` with your actual AWS Account ID.
  * Replace `aurora-postgres-cluster...` with your actual Aurora **Writer Endpoint** address.

---

## 🚀 Step 2: One-Click Deployment Command

Execute the following chained command from the root directory of the project. This applies the configuration safely in sequence, launching 2 replicas for High Availability (Active-Passive / Active-Active Failover):

```bash
kubectl apply -f deploy/k8s/secret.yaml && \
kubectl apply -f deploy/k8s/deployment.yaml && \
kubectl apply -f deploy/k8s/service.yaml
```

---

## 🔍 Step 3: Status Verification Commands

### 1. Check All Infrastructure Resources
Run the following command to verify the status of the Pods, Service, and Deployment under the exact service label:

```bash
kubectl get all -l app=exploreService
```

**📊 Expected Success Output Example:**
```text
NAME                                   READY   STATUS    RESTARTS   AGE
pod/explore-service-7fbc789b7d-2x9wl   1/1     Running   0          45s
pod/explore-service-7fbc789b7d-8z4qp   1/1     Running   0          45s

NAME                      TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)     AGE
service/explore-service   ClusterIP   10.100.15.22    <none>        50051/TCP   45s

NAME                              READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/explore-service   2/2     2            2           45s
```
*Engineering Checkpoint: Verify that the `READY` column reads `1/1` and `STATUS` is `Running`. If `RESTARTS` is greater than 0, the application is crash-looping. Check logs immediately.*

### 2. Inspect Live Container Logs
Stream the structured JSON logs from the running pods to verify database connectivity:

```bash
kubectl logs -l app=exploreService --tail=50 -f
```

---

## 🔄 Step 4: High Availability & Failover Test

This service is configured with `replicas: 2`. To verify cluster resilience when a node or instance fails, simulate a host crash by deleting one of the active pods:

```bash
# Simulating a sudden server instance crash
kubectl delete pod -l app=exploreService --max-delete-matching=1
```

**Verification:**
The Kubernetes control plane will immediately route 100% of live gRPC traffic to the remaining healthy pod while automatically spinning up a replacement pod in the background, achieving zero downtime.

---

## 🧹 Resource Cleanup

To completely tear down the microservice and its secret credentials from the cluster, execute:

```bash
kubectl delete -f deploy/k8s/service.yaml
kubectl delete -f deploy/k8s/deployment.yaml
kubectl delete -f deploy/k8s/secret.yaml
```
