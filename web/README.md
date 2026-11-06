# RelayForge Dashboard

React, Vite, Tailwind CSS, and shadcn/ui dashboard for RelayForge.

## Development

```sh
pnpm install
pnpm dev
```

The Vite dev server proxies `/api` to `http://localhost:8080`. Set
`VITE_API_URL` in `.env.local` if the API runs elsewhere.

## Build

```sh
pnpm build
```

## Features

- Registration and login with session bearer tokens
- Project dashboard with create, rename, and archive flows
- API key creation and revocation
- Job submission with JSON payload validation
- Job inspection and execution history
- Live job status refresh every two seconds while a job is active

See `../docs/` for backend behavior.
