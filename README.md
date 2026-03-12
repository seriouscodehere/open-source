# 🛡️ Open Source Security Middleware

> A lightweight, production-ready security middleware for Go applications —
> protecting your real servers behind a smart proxy with rate limiting, bot detection, brute force protection, and more.

---

## ✨ Features

| | Feature | Description |
|---|---|---|
| 🔁 | **Reverse Proxy** | Transparent upstream redirection with full control |
| 🤖 | **Bot Detection** | Identify and block automated traffic |
| 🔐 | **Brute Force Protection** | Prevent credential stuffing and login attacks |
| 🚦 | **Rate Limiting** | Strict per-API and per-endpoint request throttling |
| ✅ | **Method Validation** | Reject invalid or unexpected HTTP methods |
| 🌐 | **IP Blocking** | Block malicious or unwanted IP addresses |
| 🔑 | **Authorization Checks** | Enforce auth rules at the middleware level |
| 🍪 | **Cookie Blocking** | Intercept and block unwanted cookies |
| 🛑 | **API Misuse Prevention** | Detect and halt abusive API usage patterns |
| 📦 | **Upstream Redirection** | Route traffic to backend services seamlessly |

---

## 💡 How It Works

The middleware sits between the public internet and your real server.
Your **private server URLs stay completely hidden** — only the public proxy is exposed.
Even if someone tries to attack or guess your real APIs, they'll never reach them.

```
Attacker / Client
      │
      ▼
┌─────────────────────┐
│   Public Server     │  ← Rate limited, validated, protected
│  api.example.com    │
└────────┬────────────┘
         │  Only valid, trusted requests pass through
         ▼
┌─────────────────────┐
│   Private Server    │  ← Real data, hidden URL, never exposed
│  internal_server/   │
└─────────────────────┘
```

> 🔒 The attacker only ever sees `https://server.com/api/get/user` — never `https://private_server.com/get/user`

---

## 📁 Project Structure

```bash
open-source/
├── middleware/        # Core middleware logic
├── .gitignore
└── README.md
```

---

## 🚀 Getting Started

### Prerequisites

- Go `v1.21+`

### Installation

```bash
git clone https://github.com/seriouscodehere/open-source.git
cd open-source/middleware
go mod tidy
```

### Running

```bash
go run main.go
```

> ⚙️ Full configuration options are documented in the [`middleware/README.md`](./middleware/README.md)

---

## 🗺️ Upcoming Features

### 🔀 API URL Masking

Public APIs like `https://server.com/api/get/user` map to completely different private routes like `https://private_server.com/get/user` — attackers can never guess or reverse-engineer the real endpoint.

### 📊 IP Block Persistence

Blocked IPs stored in JSON files for long-term tracking — including which IPs are blocked most frequently and which regions they originate from.

### 🛡️ IP Trust List

Whitelist specific IPs for sensitive routes like admin dashboards and internal APIs. Even if an admin route gets leaked, unauthorized IPs simply can't access it.

### 📋 API Strict Mode

Enforce exact header and body structure on every request before it reaches the private server — required headers, allowed fields, body format validation, all configurable per route.

---

## 🤝 Contributing

Contributions are welcome and appreciated!

1. Fork the repository
2. Create a new branch: `git checkout -b feature/your-feature`
3. Commit your changes: `git commit -m 'Add your feature'`
4. Push to the branch: `git push origin feature/your-feature`
5. Open a Pull Request

Please make sure your code follows the existing style and includes relevant comments.

---

## 📄 License

This project is open source and available under the [MIT License](LICENSE).

---

## 🌟 Show Your Support

If this project helped you, please consider giving it a ⭐ on GitHub — it means a lot!

## Connect

[![YouTube](https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white)](https://www.youtube.com/@sraraaofficials)
[![LinkedIn](https://img.shields.io/badge/LinkedIn-0A66C2?style=for-the-badge&logo=linkedin&logoColor=white)](https://www.linkedin.com/in/sraraa/)
[![GitHub](https://img.shields.io/badge/GitHub-181717?style=for-the-badge&logo=github&logoColor=white)](https://github.com/sraraa)

> Built with ❤️ by Sraraa