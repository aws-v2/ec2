# SSH Keys

SSH keys provide secure, password-free authentication to your EC2 instances. Serwin uses RSA key pairs — a public key stored in the instance and a private key you keep locally.

## How SSH Keys Work

When you create an SSH key pair:
1. **Public key** — Stored on every instance that uses this key pair. Added to `~/.ssh/authorized_keys` on the VM.
2. **Private key** — Returned to you once. Used from your local machine to authenticate.

```
Your Machine                     EC2 Instance
    │                                  │
    │── ssh -i private.pem ──────────► │
    │                                  │ Checks authorized_keys
    │◄── Connection established ───── │
```

## Creating a Key Pair

### Option 1: Auto-generate (Recommended)

Let Serwin generate the key pair for you. The private key is returned in the creation response.

```bash
curl -X POST http://localhost:8080/api/v1/ec2/ssh-keys \
  -H "Content-Type: application/json" \
  -d '{"name": "my-production-key"}'
```

**Response:**
```json
{
  "data": {
    "id": 1,
    "name": "my-production-key",
    "public_key": "ssh-rsa AAAA...",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n..."
  }
}
```

> ⚠️ **Important**: The `private_key` is only returned at creation time. Save it immediately.

### Option 2: Import Your Own Public Key

If you already have an SSH key pair, import just the public key:

```bash
curl -X POST http://localhost:8080/api/v1/ec2/ssh-keys \
  -H "Content-Type: application/json" \
  -d '{
    "name": "imported-key",
    "public_key": "ssh-rsa AAAA..."
  }'
```

## Downloading the Private Key

For auto-generated keys, download the `.pem` file:

```bash
GET /api/v1/ec2/ssh-keys/:name/download
```

The response streams a `.pem` file with the correct content type for browser download.

## Connecting to Your Instance

```bash
# Set correct permissions on the key file
chmod 600 my-production-key.pem

# Connect via SSH
ssh -i my-production-key.pem ubuntu@<instance-ip>
```

## Listing Your Keys

```bash
GET /api/v1/ec2/ssh-keys
```

## Invalidating (Deleting) a Key

```bash
DELETE /api/v1/ec2/ssh-keys/:id
```

> ⚠️ Deleting a key pair does **not** revoke access for instances already using it (the public key remains in `authorized_keys`). You must manually remove it from the instance's `~/.ssh/authorized_keys`.

## Security Best Practices

- **Never share your private key** — Treat `.pem` files like passwords.
- **Set strict file permissions** — Always `chmod 600` before using a key.
- **Use one key per environment** — Separate dev/staging/production key pairs.
- **Rotate keys regularly** — Generate a new key pair periodically and update instances.
- **Disable password authentication** — Ensure `PasswordAuthentication no` is set in `/etc/ssh/sshd_config`.
- **Use SSH agent forwarding** — For bastion hops, avoid storing keys on intermediate hosts.
