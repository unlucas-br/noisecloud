# NoiseCloud Lite

<div align="center">

<pre>
    _   __      _              ________                _
   / | / /___  (_)____ ___    / ____/ /___  __  ______//
  /  |/ / __ \/ / ___/ _ \  / /   / / __ \/ / / / __  /
 / /|  / /_/ / (__  )  __/ / /___/ / /_/ / /_/ / /_/ /
/_/ |_/\____/_/____/\___/  \____/_/\____/\__,_/\__,_/
</pre>

<strong>Lite Version by Lucas Ferraz</strong>

</div>

<p align="center">
  <a href="#portugues">
    <img alt="Português" src="https://img.shields.io/badge/Portugu%C3%AAs-ativo-009739?style=for-the-badge">
  </a>
  <a href="#english">
    <img alt="English" src="https://img.shields.io/badge/English-read%20version-1f6feb?style=for-the-badge">
  </a>
</p>

<p align="center">
  <a href="https://go.dev/">
    <img alt="Feito em Go" src="https://img.shields.io/badge/Feito%20em-Go-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  </a>
  <a href="https://ffmpeg.org/">
    <img alt="FFmpeg" src="https://img.shields.io/badge/FFmpeg-required-007808?style=for-the-badge&logo=ffmpeg&logoColor=white">
  </a>
  <a href="https://github.com/unlucas-br/noisecloud/releases/tag/v1.1">
    <img alt="Release v1.1" src="https://img.shields.io/badge/release-v1.1-blue?style=for-the-badge">
  </a>
  <img alt="Licença GPL-3.0" src="https://img.shields.io/badge/licen%C3%A7a-GPL--3.0-black?style=for-the-badge">
</p>

<a id="portugues"></a>

## Português

<p align="center">
  <a href="#sobre">Sobre</a> •
  <a href="#recursos">Recursos</a> •
  <a href="#uso">Uso</a> •
  <a href="#modo-tiktok">Modo TikTok</a> •
  <a href="#notas-tecnicas">Notas Técnicas</a> •
  <a href="#english">English</a>
</p>

NoiseCloud Lite é uma ferramenta de armazenamento de dados em vídeo por codificação visual. Ela converte arquivos de qualquer formato em fluxos de pixels, organizando dados binários em frames de ruído visual para transporte e armazenamento em vídeo.

A versão Lite foca em uso simples, menu interativo, aceleração por hardware e reconstrução resiliente. A versão `v1.1` adiciona **suporte ao TikTok**, com encode/decode dedicado para vídeos verticais, preset próprio, autocalibração no decode e correção de erros mais resistente a resize e recompressão.

Autor: **Lucas Ferraz**  
Linguagem: **Go**  
Licença: **GPL-3.0**

---

## Sobre

O NoiseCloud Lite foi criado para transformar um arquivo comum em um vídeo `.mp4` contendo dados codificados visualmente. No encode, o arquivo é comprimido com Gzip, dividido em frames, protegido com Reed-Solomon e renderizado como macropixels em tons de cinza. No decode, o vídeo é extraído frame a frame, calibrado e reconstruído até recuperar o arquivo original.

O projeto não depende de servidor, conta externa ou banco de dados. O uso principal é local, por terminal, com FFmpeg cuidando da etapa de vídeo.

---

## Recursos

### Encode e decode

- Encode de arquivos para vídeo `.mp4`
- Decode para recuperar o arquivo original
- Compressão automática com Gzip antes do encode
- Reconstrução do payload bruto antes da descompressão final
- Arquivo de saída protegido contra gravação corrompida quando a validação falha

### Vídeo e performance

- Suporte a FFmpeg
- Aceleração por GPU com NVIDIA NVENC, AMD AMF e Intel QSV
- Fallback para CPU com `libx264`
- Processamento paralelo com múltiplas threads
- Presets de grade para modo padrão e modo TikTok

### Resiliência

- Reed-Solomon para correção de erros por frame
- Macropixels binários de alto contraste (`0/255`)
- Barra de calibração por frame
- Decode com autocalibração de limiar
- Tentativas alternativas de preset, alinhamento e ECC no reconstrutor

> [!NOTE]
> A versão `v1.1` é a primeira release com suporte ao TikTok: encode vertical `9:16`, decode TikTok e reconstrução adaptada a vídeos baixados/recomprimidos pela plataforma.

O executável não deve ser commitado no repositório. Binários públicos são distribuídos como assets de Release.

---

## Pré-Requisitos

Para usar o NoiseCloud Lite, você precisa do FFmpeg instalado e acessível no sistema.

> [!TIP]
> No Windows, abra o PowerShell como Administrador e instale com:
>
> ```powershell
> winget install -e --id Gyan.FFmpeg
> ```

---

## Uso

Execute o programa dentro da pasta onde está o `ncc.exe`:

```powershell
.\ncc.exe
```

Ou, se o executável estiver no `PATH`:

```bash
ncc
```

No menu interativo, escolha a ação desejada:

![Demonstração de Uso](img/noisecloud.gif)

| Opção | Função |
|---|---|
| `1` | Encode padrão para esconder um arquivo em vídeo |
| `2` | Encode TikTok em vídeo vertical 9:16 |
| `3` | Decode padrão para recuperar arquivo |
| `4` | Decode TikTok com grade/autocalibração dedicada |
| `5` | Sair |

Recomendação: deixe o arquivo de entrada na mesma pasta do executável para simplificar o uso pelo menu.

---

## Modo TikTok

O modo TikTok usa um preset vertical pensado para sobreviver melhor ao resize e à recompressão da plataforma.

Parâmetros principais:

| Item | Valor |
|---|---|
| Resolução de encode | `1080x1920` |
| Proporção | `9:16` |
| FPS | `30` |
| MacroSize | `45` no encode, aproximadamente `24px` após resize comum |
| ECC | `10` data shards + `6` parity shards |
| Cores binárias | `0` e `255` |

No decode, o reconstrutor detecta a dimensão real do vídeo extraído, recalcula a grade, usa a barra de calibração e tenta variações de alinhamento, limiar, preset e ECC. Isso ajuda quando o vídeo foi baixado do TikTok em outra resolução, como `576x1024`.

---

## Notas Técnicas

- O formato visual usa frames com cabeçalho `NCC3`, índice do frame, total de frames e tamanho de payload.
- O payload é protegido por Reed-Solomon antes de ser renderizado.
- A barra de calibração ajuda o decoder a estimar preto/branco após compressão H.264.
- O modo TikTok é mais robusto, mas plataformas de vídeo continuam sendo canais hostis: corte, crop, mudança brusca de FPS ou filtros podem inviabilizar a recuperação.
- NoiseCloud Lite não é criptografia. Ele transforma e transporta dados visualmente; para sigilo real, criptografe o arquivo antes do encode.

---

## Estrutura do Projeto

```text
cmd/cli/           menu interativo e fluxo encode/decode
internal/encoder/  renderização de frames, ECC e pipeline FFmpeg
internal/decoder/  extração de frames e reconstrução do payload
pkg/utils/         utilitários compartilhados
css/, js/, img/    página e assets de apresentação
```

---

## Contribuição

Contribuições são bem-vindas em testes reais de upload/download, ajustes de preset, documentação, portabilidade e melhorias de reconstrução.

---

## Licença

GPL-3.0. Veja o arquivo [`LICENSE`](LICENSE).

---

<a id="english"></a>

## English

<p align="center">
  <a href="#portugues">
    <img alt="Português" src="https://img.shields.io/badge/Portugu%C3%AAs-ler%20vers%C3%A3o-009739?style=for-the-badge">
  </a>
  <a href="#english">
    <img alt="English" src="https://img.shields.io/badge/English-active-1f6feb?style=for-the-badge">
  </a>
</p>

<p align="center">
  <a href="#about">About</a> •
  <a href="#features">Features</a> •
  <a href="#usage">Usage</a> •
  <a href="#tiktok-mode">TikTok Mode</a> •
  <a href="#technical-notes">Technical Notes</a> •
  <a href="#portugues">Português</a>
</p>

NoiseCloud Lite is a video-based data storage tool built around visual encoding. It converts files of any format into pixel streams, arranging binary data as visual noise frames for video transport and storage.

The Lite version focuses on simple usage, an interactive menu, hardware acceleration and resilient reconstruction. Version `v1.1` adds **TikTok support**, with dedicated vertical-video encode/decode, its own preset, decode autocalibration and stronger error recovery against resize and recompression.

Author: **Lucas Ferraz**  
Language: **Go**  
License: **GPL-3.0**

---

## About

NoiseCloud Lite turns a regular file into an `.mp4` video containing visually encoded data. During encode, the file is compressed with Gzip, split into frames, protected with Reed-Solomon and rendered as grayscale macropixels. During decode, the video is extracted frame by frame, calibrated and reconstructed until the original file is recovered.

The project does not depend on a server, external account or database. The main workflow is local and terminal-based, with FFmpeg handling the video stage.

---

## Features

### Encode and decode

- Encode files into `.mp4` videos
- Decode videos to recover the original file
- Automatic Gzip compression before encode
- Raw payload reconstruction before final decompression
- Output file protection when final validation fails

### Video and performance

- FFmpeg support
- GPU acceleration with NVIDIA NVENC, AMD AMF and Intel QSV
- CPU fallback with `libx264`
- Parallel processing with multiple threads
- Grid presets for standard mode and TikTok mode

### Resilience

- Reed-Solomon error correction per frame
- High-contrast binary macropixels (`0/255`)
- Per-frame calibration bar
- Threshold autocalibration during decode
- Alternative preset, alignment and ECC attempts in the reconstructor

> [!NOTE]
> Version `v1.1` is the first release with TikTok support: vertical `9:16` encode, TikTok decode and reconstruction adapted to videos downloaded/recompressed by the platform.

The executable should not be committed to the repository. Public binaries are distributed as Release assets.

---

## Requirements

NoiseCloud Lite requires FFmpeg installed and available on the system.

> [!TIP]
> On Windows, open PowerShell as Administrator and install it with:
>
> ```powershell
> winget install -e --id Gyan.FFmpeg
> ```

---

## Usage

Run the program inside the folder containing `ncc.exe`:

```powershell
.\ncc.exe
```

Or, if the executable is available in your `PATH`:

```bash
ncc
```

In the interactive menu, choose the desired action:

![Usage Demo](img/noisecloud.gif)

| Option | Purpose |
|---|---|
| `1` | Standard encode to hide a file in video |
| `2` | TikTok encode in vertical 9:16 video |
| `3` | Standard decode to recover a file |
| `4` | TikTok decode with dedicated grid/autocalibration |
| `5` | Exit |

Recommendation: keep the input file in the same folder as the executable to simplify menu usage.

---

## TikTok Mode

TikTok mode uses a vertical preset designed to better survive platform resize and recompression.

Main parameters:

| Item | Value |
|---|---|
| Encode resolution | `1080x1920` |
| Aspect ratio | `9:16` |
| FPS | `30` |
| MacroSize | `45` during encode, around `24px` after common resize |
| ECC | `10` data shards + `6` parity shards |
| Binary colors | `0` and `255` |

During decode, the reconstructor detects the real extracted video size, recalculates the grid, uses the calibration bar and tries alignment, threshold, preset and ECC variations. This helps when a TikTok download comes back at another resolution, such as `576x1024`.

---

## Technical Notes

- The visual format uses frames with an `NCC3` header, frame index, total frame count and payload size.
- The payload is protected by Reed-Solomon before rendering.
- The calibration bar helps the decoder estimate black/white levels after H.264 compression.
- TikTok mode is more robust, but video platforms remain hostile channels: cuts, crops, aggressive FPS changes or filters can make recovery impossible.
- NoiseCloud Lite is not encryption. It visually transforms and transports data; for real secrecy, encrypt the file before encoding it.

---

## Project Structure

```text
cmd/cli/           interactive menu and encode/decode flow
internal/encoder/  frame rendering, ECC and FFmpeg pipeline
internal/decoder/  frame extraction and payload reconstruction
pkg/utils/         shared utilities
css/, js/, img/    presentation page and assets
```

---

## Contributing

Contributions are welcome in real upload/download testing, preset tuning, documentation, portability and reconstruction improvements.

---

## License

GPL-3.0. See [`LICENSE`](LICENSE).
