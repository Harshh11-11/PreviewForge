'use client'

import { useEffect, useRef, useState } from 'react'

export default function Home() {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  const [repo, setRepo] = useState('')
  const [status, setStatus] = useState('')
  const [previewUrl, setPreviewUrl] = useState('')

  // Render backend URL comes from Vercel environment variables
  const API_URL = process.env.NEXT_PUBLIC_API_URL

  useEffect(() => {
    const canvas = canvasRef.current

    if (!canvas) return

    const ctx = canvas.getContext('2d')

    if (!ctx) return

    const activeCanvas = canvas
    const drawingContext = ctx

    let animId: number

    const resizeCanvas = () => {
      canvas.width = window.innerWidth
      canvas.height = window.innerHeight
    }

    resizeCanvas()

    window.addEventListener('resize', resizeCanvas)

    const particles = Array.from({ length: 120 }, () => ({
      x: Math.random() * canvas.width,
      y: Math.random() * canvas.height,
      size: Math.random() * 1.5 + 0.3,
      sx: (Math.random() - 0.5) * 0.4,
      sy: (Math.random() - 0.5) * 0.4,
      alpha: Math.random() * 0.6 + 0.1,
      pulse: Math.random() * Math.PI * 2,
    }))

    function loop() {
      drawingContext.fillStyle = 'rgba(2,12,8,0.15)'
      drawingContext.fillRect(0, 0, activeCanvas.width, activeCanvas.height)

      particles.forEach((p) => {
        p.x += p.sx
        p.y += p.sy
        p.pulse += 0.02

        if (
          p.x < 0 ||
          p.x > activeCanvas.width ||
          p.y < 0 ||
          p.y > activeCanvas.height
        ) {
          p.x = Math.random() * activeCanvas.width
          p.y = Math.random() * activeCanvas.height
        }

        drawingContext.beginPath()
        drawingContext.arc(p.x, p.y, p.size, 0, Math.PI * 2)

        drawingContext.fillStyle = `rgba(0,255,136,${
          p.alpha * (0.6 + 0.4 * Math.sin(p.pulse))
        })`

        drawingContext.fill()
      })

      animId = requestAnimationFrame(loop)
    }

    loop()

    return () => {
      cancelAnimationFrame(animId)
      window.removeEventListener('resize', resizeCanvas)
    }
  }, [])

  function pollStatus(id: string) {
    const interval = setInterval(async () => {
      try {
        if (!API_URL) {
          clearInterval(interval)
          setStatus('> API URL is not configured')
          return
        }

        const res = await fetch(
          `${API_URL}/api/status?id=${encodeURIComponent(id)}`
        )

        if (!res.ok) {
          throw new Error(`Status request failed: ${res.status}`)
        }

        const data = await res.json()

        console.log('Status:', data)

        if (data.status === 'ready') {
          clearInterval(interval)

          setStatus('✓ preview ready')
          setPreviewUrl(data.url)
        } else if (data.status === 'failed') {
          clearInterval(interval)

          setStatus(
            `> preview failed: ${data.message ?? 'unknown error'}`
          )
        } else {
          setStatus('> building... please wait')
        }
      } catch (error) {
        console.error('Status error:', error)

        clearInterval(interval)
        setStatus('> error checking status')
      }
    }, 2000)
  }

  async function forge() {
    if (!repo.trim()) {
      setStatus('> enter a github url first')
      return
    }

    if (!API_URL) {
      setStatus('> backend URL is not configured')
      console.error(
        'NEXT_PUBLIC_API_URL is not defined'
      )
      return
    }

    setPreviewUrl('')
    setStatus('> connecting to forge engine...')

    try {
      const res = await fetch(
        `${API_URL}/api/preview`,
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            repoUrl: repo.trim(),
          }),
        }
      )

      if (!res.ok) {
        const text = await res.text()

        throw new Error(
          `Backend returned ${res.status}: ${text}`
        )
      }

      const data = await res.json()

      console.log('Preview response:', data)

      if (!data.id) {
        throw new Error('Backend did not return a preview ID')
      }

      setStatus(
        `> cloning started [id: ${data.id}]`
      )

      pollStatus(data.id)
    } catch (error) {
      console.error('Forge error:', error)

      setStatus(
        '> error: could not reach backend'
      )
    }
  }

  return (
    <main className="relative min-h-screen bg-[#020c08] flex flex-col items-center justify-start overflow-x-hidden">
      <canvas
        ref={canvasRef}
        aria-hidden="true"
        className="pointer-events-none fixed inset-0 h-full w-full"
      />

      <div className="relative z-10 flex flex-col items-center gap-4 pt-16 w-full px-4">
        <span className="border border-[#00ff8844] text-[#00ff8888] text-xs px-4 py-1 rounded-full tracking-widest font-mono">
          PREVIEW · FORGE · ENGINE
        </span>

        <h1
          style={{
            fontFamily: 'cursive',
            textShadow:
              '0 0 20px #00ff88, 0 0 60px #009944',
          }}
          className="text-6xl text-[#00ff88]"
        >
          PreviewForge
        </h1>

        <p
          style={{ fontFamily: 'cursive' }}
          className="text-[#39ff99] text-xl"
        >
          paste a repo. get a live preview. instantly.
        </p>

        <input
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              forge()
            }
          }}
          className="w-full max-w-96 px-4 py-3 bg-[#020c08] border border-[#00ff88] text-[#00ff88] font-mono text-sm rounded focus:outline-none placeholder-[#1a6640]"
          placeholder="https://github.com/user/repo"
        />

        <button
          onClick={forge}
          className="w-full max-w-96 py-3 border border-[#00ff88] text-[#00ff88] font-mono tracking-widest hover:bg-[#00ff88] hover:text-[#020c08] transition-all rounded"
        >
          ⚡ GENERATE PREVIEW
        </button>

        <p
          className="font-mono text-sm text-[#00ff88] h-5"
          style={{
            textShadow: '0 0 6px #00ff88',
          }}
        >
          {status}
        </p>

        {previewUrl && (
          <div className="flex flex-col items-center gap-2 w-full mt-2 pb-16">
            <div className="flex items-center gap-3">
              <span className="font-mono text-xs text-[#00ff8888] tracking-widest">
                LIVE PREVIEW
              </span>

              <a
                href={previewUrl}
                target="_blank"
                rel="noreferrer"
                className="font-mono text-xs text-[#00ff88] underline hover:text-[#00cc66]"
              >
                {previewUrl} ↗
              </a>
            </div>

            <div
              className="rounded overflow-hidden"
              style={{
                width: '90vw',
                maxWidth: '1100px',
                height: '600px',
                border: '1px solid #00ff8844',
                boxShadow:
                  '0 0 30px #00ff8822',
              }}
            >
              <div className="flex items-center gap-2 px-3 py-2 border-b border-[#00ff8822] bg-[#020c08]">
                <div className="w-2 h-2 rounded-full bg-[#00ff88]" />

                <span className="font-mono text-xs text-[#00ff8866] truncate">
                  {previewUrl}
                </span>
              </div>

              <iframe
                src={previewUrl}
                title="Live Preview"
                width="100%"
                height="100%"
                style={{
                  border: 'none',
                  background: '#fff',
                }}
              />
            </div>
          </div>
        )}
      </div>
    </main>
  )
}
