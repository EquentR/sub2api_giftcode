import http from 'node:http'

const port = Number(process.env.MOCK_PORT || 18081)
const accounts = new Map()
for (const account of [
  { id: 1, name: 'primary-pool', platform: 'openai', type: 'apikey', status: 'active', schedulable: true, extra: {}, credentials: { model_mapping: { 'gpt-5': 'gpt-5', o3: 'o3' }, upstream_supported_models: ['gpt-5', 'o3'] } },
  { id: 2, name: 'high-cost', platform: 'openai', type: 'apikey', status: 'active', schedulable: false, extra: {}, credentials: { model_mapping: { 'gpt-5': 'gpt-5', o3: 'o3' }, upstream_supported_models: ['gpt-5', 'o3'] } },
]) {
  accounts.set(account.id, account)
}

const json = (res, status, data) => {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify({ code: status >= 300 ? status : 0, message: status >= 300 ? 'mock upstream error' : 'success', data }))
}

http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost')
  if (req.method === 'POST' && url.pathname === '/api/v1/auth/login') {
    json(res, 200, {
      access_token: 'mock-access-token',
      refresh_token: '',
      expires_in: 86400,
      token_type: 'Bearer',
      user: { id: 1, email: 'admin@example.com', username: 'admin', role: 'admin', status: 'active' },
    })
    return
  }
  if (req.method === 'GET' && url.pathname === '/api/v1/auth/me') {
    json(res, 200, { id: 1, email: 'admin@example.com', username: 'admin', role: 'admin', status: 'active' })
    return
  }
  if (req.method === 'GET' && url.pathname === '/api/v1/admin/accounts') {
    json(res, 200, { items: [...accounts.values()], total: accounts.size, page: 1, page_size: 200, pages: 1 })
    return
  }
  const accountMatch = url.pathname.match(/^\/api\/v1\/admin\/accounts\/(\d+)$/)
  if (req.method === 'GET' && accountMatch) {
    const account = accounts.get(Number(accountMatch[1]))
    if (!account) {
      json(res, 404, null)
      return
    }
    json(res, 200, account)
    return
  }
  const schedulableMatch = url.pathname.match(/^\/api\/v1\/admin\/accounts\/(\d+)\/schedulable$/)
  if (req.method === 'POST' && schedulableMatch) {
    let body = ''
    req.on('data', (chunk) => { body += chunk })
    req.on('end', () => {
      const parsed = JSON.parse(body || '{}')
      const account = accounts.get(Number(schedulableMatch[1]))
      if (!account) {
        json(res, 404, null)
        return
      }
      const updated = { ...account, schedulable: Boolean(parsed.schedulable) }
      accounts.set(account.id, updated)
      json(res, 200, updated)
    })
    return
  }
  json(res, 404, null)
}).listen(port, '127.0.0.1', () => {
  console.log(`mock sub2api listening on ${port}`)
})
