# Terraform Remote Backend Bootstrap Guide

This directory contains the baseline Terraform configuration required to provision the **AWS S3 Bucket** and **Amazon DynamoDB Table**. These resources serve as the remote state backend and distributed locking mechanism for your main `exploreService` infrastructure pipelines.

> ⚠️ **CRITICAL ENGINEERING WARNING**: This bootstrap process must be executed **manually, once, and only from a local engineer's workstation**. It must never be bundled into the same runtime execution folder as your application infrastructure (`main.tf`), or it will induce an unrecoverable state-locking deadlock.

---

## 🛠️ Step 1: Initialize the Bootstrap Configuration

Navigate to your bootstrap directory and initialize the local active `.tf` files from the provided templates:

```bash
cd terraform-bootstrap/
cp bootstrap.tf.example bootstrap.tf
```

### Configuration Updates Required:
Open `bootstrap.tf` and update the placeholders to match your organization's infrastructure naming convention:
* **`bucket`**: Replace `your-company-terraform-state-bucket` with a globally unique name (S3 bucket names must be unique across all AWS accounts globally).
* **`name` (DynamoDB)**: Replace `your-company-terraform-locks-table` with your desired state locking table name.

---

## 🚀 Step 2: Provision the Storage and Lock Infrastructure

Run the following commands sequentially to initialize a temporary local state workspace, inspect the creation block, and apply changes directly to AWS:

```bash
# 1. Download the required AWS provider plugin
terraform init

# 2. Preview the AWS S3 and DynamoDB resources to be created
terraform plan

# 3. Apply the changes to create the remote state storage infrastructure
terraform apply --auto-approve
```

---

## 🔒 Step 3: Verifying the Bootstrap Output

Once the setup completes successfully, verify that your backend assets are alive in your AWS console or via the AWS CLI:

```bash
# Check if the S3 bucket is active and secure
aws s3api get-bucket-versioning --bucket your-company-terraform-state-bucket

# Check if the DynamoDB table is online
aws dynamodb describe-table --table-name your-company-terraform-locks-table --query "Table.TableStatus"
```
*Expected Output: Bucket versioning should report `Enabled`, and the DynamoDB status should report `"ACTIVE"`.*

---

## 🔄 Step 4: Activating the Remote Backend in the Automation Branch

Now that the S3 bucket and DynamoDB table are alive on AWS, you can safely configure the `github-actions-deploy` branch to use them:

1. Switch to your automation branch: `git checkout github-actions-deploy`
2. Update the `backend "s3"` block inside `terraform/providers.tf` with the **exact** bucket and table names you just created.
3. Run `terraform init` inside the application `terraform/` directory. Terraform will detect the change and ask:
   *"Do you want to copy existing state to the new backend?"*
4. Type **`yes`**. Your application state is now migrated to the cloud, and your GitHub Actions runner can safely manage deployment without state loss.
