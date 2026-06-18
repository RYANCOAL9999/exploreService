## 💡 Critical Operational Hints & Production Guardrails

### 1. Secret Protection Mechanism (`secrets.AURORA_DB_PASSWORD`)
To enforce compliance with DevSecOps standards, sensitive database credentials must never be hardcoded in any configuration file or workflow declaration. 
* **Action Required**: You must navigate to your GitHub Repository -> **Settings** -> **Secrets and variables** -> **Actions** -> click **New repository secret**. 
* **Configuration**: Register a key explicitly named `AURORA_DB_PASSWORD` and insert your raw production password as the value. 
* **Under the Hood**: GitHub automatically redacts secrets using cryptographic encryption. The value is masked (`***`) inside all workflow runners and runner logs to prevent credential leakage.

### 2. The Terraform State Loss & Locking Hazard (Crucial for CI/CD)
When running `terraform apply` locally, Terraform writes a local file named `terraform.tfstate` to track infrastructure mappings. However, **GitHub Actions uses ephemeral (temporary) virtual machines**. Once the workflow completes, the local virtual machine is destroyed, causing complete loss of your state metadata!
* **Production Recommendation**: You must transition from a local state to a **Remote Backend**. 
* **Implementation Plan**: Update your `providers.tf.example` block to store the state file remotely inside an **AWS S3 Bucket** and configure a **DynamoDB Table** for state locking:

```hcl
# Add this code block inside your active providers.tf file for production
terraform {
  backend "s3" {
    bucket         = "your-company-terraform-state-bucket"
    key            = "exploreService/production/terraform.tfstate"
    region         = "ap-east-1"
    dynamodb_table = "your-company-terraform-locks-table" # Enforces write-locking
    encrypt        = true
  }
}
```
* **Why This Matters**: The DynamoDB lock prevents race conditions (e.g., two concurrent developers or multiple automated GitHub commits triggering simultaneous modifications to your EKS infrastructure).
