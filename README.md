[![kado-proxy CI/CD](https://github.com/janpreet/kado-proxy/actions/workflows/kado-proxy.yaml/badge.svg)](https://github.com/janpreet/kado-proxy/actions/workflows/kado-proxy.yaml)[![Go Report Card](https://goreportcard.com/badge/github.com/janpreet/kado-proxy)](https://goreportcard.com/report/github.com/janpreet/kado-proxy)
# kado-proxy

kado-proxy is a lightweight, Go-based proxy designed to sit between your tools and the GitHub API. It helps manage API rate limits and handles authentication, ensuring your applications can interact with GitHub more reliably and securely.

## Features

- Manages GitHub API rate limits
- Supports both Personal Access Token and GitHub App authentication
- Implements HTTPS for secure communication
- Easy to deploy using Docker or Kubernetes
- Can be integrated with various tools that interact with GitHub

## Installation

### Using Go

```bash
go get github.com/janpreet/kado-proxy
```

### Using Docker

```bash
docker pull ghcr.io/janpreet/kado-proxy:latest
```

## Usage

### Running kado-proxy

#### Using Go

```bash
kado-proxy -cert=/path/to/cert.pem -key=/path/to/key.pem -port=8443
```

#### Using Docker

```bash
docker run -d -p 8443:8443 \
  -v /path/to/cert.pem:/etc/kado-proxy/cert.pem \
  -v /path/to/key.pem:/etc/kado-proxy/key.pem \
  ghcr.io/janpreet/kado-proxy:latest \
  -cert=/etc/kado-proxy/cert.pem \
  -key=/etc/kado-proxy/key.pem \
  -port=8443
```

### Integrating with Your Project

To use kado-proxy in your project, set your GitHub API base URL to `https://localhost:8443` (or wherever you're hosting kado-proxy) instead of `https://api.github.com`.

## Authentication

kado-proxy supports two methods of authentication with GitHub: Personal Access Tokens (PAT) and GitHub Apps.

### Personal Access Tokens

If you're using a Personal Access Token (PAT), configure your tool to use the token as usual. kado-proxy will forward the Authorization header containing the token to GitHub.

### GitHub Apps

To use GitHub App authentication:

1. Set the following environment variables when running kado-proxy:
   - `GITHUB_APP_ID`: Your GitHub App's ID
   - `GITHUB_APP_PRIVATE_KEY`: Your GitHub App's private key
   - `GITHUB_INSTALLATION_ID`: The installation ID for your GitHub App

2. kado-proxy will automatically handle JWT generation and token exchange.

## Certificate Setup

### Self-Signed Certificates (for testing)

1. Generate a self-signed certificate:
   ```bash
   openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
   ```

2. Use these files with the `-cert` and `-key` flags when running kado-proxy.

### Let's Encrypt (for production)

1. Install certbot:
   ```bash
   sudo apt-get update
   sudo apt-get install certbot
   ```

2. Obtain a certificate:
   ```bash
   sudo certbot certonly --standalone -d your-domain.com
   ```

3. Use the generated certificates with kado-proxy:
   ```bash
   kado-proxy -cert=/etc/letsencrypt/live/your-domain.com/fullchain.pem \
              -key=/etc/letsencrypt/live/your-domain.com/privkey.pem
   ```

### Kubernetes with cert-manager

1. Install cert-manager in your cluster.

2. Create an Issuer or ClusterIssuer.

3. Create a Certificate resource:
   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata:
     name: kado-proxy-cert
     namespace: your-namespace
   spec:
     secretName: kado-proxy-tls
     issuerRef:
       name: your-issuer
       kind: Issuer
     commonName: kado-proxy.your-domain.com
     dnsNames:
     - kado-proxy.your-domain.com
   ```

4. Update your kado-proxy Deployment to use this secret.

### Cloud-Managed Certificates

#### Google Cloud (GKE)

1. Create a managed certificate:
   ```yaml
   apiVersion: networking.gke.io/v1
   kind: ManagedCertificate
   metadata:
     name: kado-proxy-cert
   spec:
     domains:
       - kado-proxy.your-domain.com
   ```

2. Annotate your Ingress to use this certificate.

#### AWS (EKS)

1. Request a certificate in AWS Certificate Manager.

2. Use the ARN of this certificate in your Ingress or ALB Ingress Controller configuration.

## Example: Using kado-proxy with Atlantis in Kubernetes

1. Deploy kado-proxy:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kado-proxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kado-proxy
  template:
    metadata:
      labels:
        app: kado-proxy
    spec:
      containers:
      - name: kado-proxy
        image: ghcr.io/janpreet/kado-proxy:latest
        args:
        - "-cert=/etc/kado-proxy-tls/tls.crt"
        - "-key=/etc/kado-proxy-tls/tls.key"
        - "-port=8443"
        ports:
        - containerPort: 8443
        volumeMounts:
        - name: kado-proxy-tls
          mountPath: "/etc/kado-proxy-tls"
          readOnly: true
        env:
        - name: GITHUB_APP_ID
          valueFrom:
            secretKeyRef:
              name: github-app-credentials
              key: app-id
        - name: GITHUB_APP_PRIVATE_KEY
          valueFrom:
            secretKeyRef:
              name: github-app-credentials
              key: private-key
        - name: GITHUB_INSTALLATION_ID
          valueFrom:
            secretKeyRef:
              name: github-app-credentials
              key: installation-id
      volumes:
      - name: kado-proxy-tls
        secret:
          secretName: kado-proxy-tls
---
apiVersion: v1
kind: Service
metadata:
  name: kado-proxy
spec:
  selector:
    app: kado-proxy
  ports:
    - protocol: TCP
      port: 443
      targetPort: 8443
```

2. Configure Atlantis:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: atlantis
spec:
  template:
    spec:
      containers:
      - name: atlantis
        env:
        - name: ATLANTIS_GH_HOSTNAME
          value: "kado-proxy"
        - name: ATLANTIS_GH_URL
          value: "https://kado-proxy"
```

## Security Considerations

- Always use HTTPS in production environments.
- Ensure that certificates and private keys are stored securely.
- Regularly rotate GitHub App private keys and Personal Access Tokens.
- Implement network policies in Kubernetes to restrict access to kado-proxy.

## Contributing

Contributions to kado-proxy are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License.

## Support

If you encounter any issues or have questions, please file an issue on the GitHub repository.