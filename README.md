# NoiseCloud 2.0

<div align="center">

<pre>
    _   __      _              ________                _
   / | / /___  (_)____ ___    / ____/ /___  __  ______//
  /  |/ / __ \/ / ___/ _ \  / /   / / __ \/ / / / __  /
 / /|  / /_/ / (__  )  __/ / /___/ / /_/ / /_/ / /_/ /
/_/ |_/\____/_/____/\___/  \____/_/\____/\__,_/\__,_/
</pre>

<strong>Version 2.0 by Lucas Ferraz</strong>

</div>

<p align="center">
  <a href="#portugues">
    <img alt="Português" src="https://img.shields.io/badge/Portugues-ativo-009739?style=for-the-badge">
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
  <a href="https://github.com/unlucas-br/noisecloud/releases/tag/v2.0">
    <img alt="Release v2.0" src="https://img.shields.io/badge/release-v2.0-blue?style=for-the-badge">
  </a>
  <img alt="Licença GPL-3.0" src="https://img.shields.io/badge/licenca-GPL--3.0-black?style=for-the-badge">
</p>

<a id="portugues"></a>

## Português

<p align="center">
  <a href="#sobre">Sobre</a> |
  <a href="#novidades-da-20">Novidades da 2.0</a> |
  <a href="#recursos">Recursos</a> |
  <a href="#uso">Uso</a> |
  <a href="#modo-tiktok">Modo TikTok</a> |
  <a href="#notas-tecnicas">Notas Técnicas</a> |
  <a href="#english">English</a>
</p>

NoiseCloud 2.0 é uma ferramenta de armazenamento de dados em vídeo por codificação visual. Ela converte arquivos de qualquer formato em frames de ruído visual, para transporte e recuperação posterior em `.mp4`.

A principal diferença desta release é a troca do motor público: o fluxo padrão agora usa o codec `Weave 1.0` (`WEV1`) com blocos de dados e rescue frames, em vez do pipeline anterior centrado em Reed-Solomon por frame. O modo TikTok continua presente, e a experiência de terminal foi simplificada para o uso diário.

Autor: **Lucas Ferraz**  
Linguagem: **Go**  
Licença: **GPL-3.0**

---

## Sobre

O NoiseCloud 2.0 transforma um arquivo comum em um vídeo `.mp4` com dados codificados visualmente. No encode, o arquivo é comprimido com Gzip, empacotado em blocos do motor `Weave 1.0` (`WEV1`), convertido em macropixels de alto contraste e renderizado em vídeo com FFmpeg. No decode, o vídeo é extraído frame a frame, calibrado, reconstruído e descomprimido, até recuperar o arquivo original.

O projeto não depende de servidor, conta externa ou banco de dados. O uso principal continua sendo local, por terminal, com FFmpeg cuidando da etapa de vídeo.

---

## Novidades da 2.0

- Motor público padrão trocado para `Weave 1.0 / WEV1`
- Proteção por blocos com `16` data frames + `2` rescue frames
- Preset padrão mais compacto em `640x360 @ 30 fps`
- Preset TikTok mantido em `1080x1920 @ 30 fps`
- Decode padrão com leitura de trailer `WEV1` antes da reconstrução por frames
- Terminal mais limpo, sem banner verboso do FFmpeg no fluxo principal
- Menu interativo revisado com mensagens de progresso mais claras

Em relação ao projeto original publicado como `NoiseCloud Lite`, esta versão prioriza:

- menor custo visual no modo padrão
- fluxo padrão mais simples para encode e decode
- documentação alinhada ao motor atual
- separação mais clara entre modo padrão e modo TikTok

---

## Recursos

### Encode e decode

- Encode de arquivos para vídeo `.mp4`
- Decode para recuperar o arquivo original
- Compressão automática com Gzip antes do encode
- Reconstrução do payload bruto antes da descompressão final
- Validação do arquivo recuperado antes de concluir a operação

### Vídeo e processamento

- Suporte a FFmpeg
- Encode padrão `weave` em `640x360`
- Encode TikTok em `1080x1920`
- Renderização H.264 com `faststart`
- Pipeline concorrente para renderização e reconstrução

### Resiliência

- Empacotamento `WEV1` com blocos de `16+2`
- Macropixels binários de alto contraste (`0/255`)
- Barra de calibração por frame
- Decode com autocalibração de limiar
- Tentativas alternativas de alinhamento e reconstrução no decoder

> [!NOTE]
> O modo TikTok continua experimental. Ele foi mantido por ser mais tolerante a resize e recompressão em vídeo vertical, mas plataformas de vídeo seguem sendo canais hostis.

O executável não deve ser commitado no repositório. Binários públicos devem ser distribuídos como assets de Release.

---

## Pré-requisitos

Para usar o NoiseCloud 2.0, você precisa do FFmpeg instalado e acessível no sistema.

> [!TIP]
> No Windows, abra o PowerShell como Administrador e instale com:
>
> ```powershell
> winget install -e --id Gyan.FFmpeg
> ```

---

## Uso

Execute o programa dentro da pasta onde está o executável:

```powershell
.\ncc.exe
```

Ou, se ele estiver no `PATH`:

```bash
ncc
```

No menu interativo, escolha a ação desejada:

![Demonstração de Uso](img/noisecloud.gif)

| Opção | Função |
|---|---|
| `1` | Encode padrão com motor Weave |
| `2` | Encode TikTok em vídeo vertical `9:16` |
| `3` | Decode padrão com motor Weave |
| `4` | Decode TikTok com grade/autocalibração dedicada |
| `5` | Sair |

Recomendação: deixe o arquivo de entrada na mesma pasta do executável, para simplificar o uso pelo menu.

---

## Modo TikTok

O modo TikTok usa um preset vertical pensado para sobreviver melhor a resize e recompressão.

Parâmetros principais:

| Item | Valor |
|---|---|
| Resolução de encode | `1080x1920` |
| Proporção | `9:16` |
| FPS | `30` |
| MacroSize | `45` no encode |
| Gray Levels | `2` |
| Calibração | barra dedicada no topo do frame |

No decode, o reconstrutor detecta a dimensão real do vídeo extraído, recalcula a grade, usa a barra de calibração e tenta variações de alinhamento e limiar. Isso ajuda quando o vídeo volta com outra resolução, após upload e download.

---

## Notas Técnicas

- O formato `WEV1` organiza o payload em blocos com frames de dados e rescue frames.
- O preset padrão `weave` usa `640x360`, `MacroSize 8`, `30 fps` e `GrayLevels 2`.
- O preset TikTok usa `1080x1920`, `MacroSize 45`, `30 fps` e grade vertical dedicada.
- O decoder tenta ler trailer `WEV1` antes de cair para a reconstrução visual por frames.
- A barra de calibração ajuda o decoder a estimar preto e branco após compressão H.264.
- NoiseCloud 2.0 não é criptografia. Para sigilo real, criptografe o arquivo antes do encode.

---

## Estrutura do Projeto

```text
cmd/cli/           menu interativo e fluxo encode/decode
internal/encoder/  renderização de frames, pipeline FFmpeg e Weave
internal/decoder/  extração de frames, trailer e reconstrução
internal/weave/    configuração e formato do motor WEV1
pkg/utils/         utilitários compartilhados
css/, js/, img/    página e assets de apresentação
```

## Licença

GPL-3.0. Veja o arquivo [`LICENSE`](LICENSE).

---

<a id="english"></a>

## English

<p align="center">
  <a href="#portugues">
    <img alt="Português" src="https://img.shields.io/badge/Portugues-read%20version-009739?style=for-the-badge">
  </a>
  <a href="#english">
    <img alt="English" src="https://img.shields.io/badge/English-active-1f6feb?style=for-the-badge">
  </a>
</p>

<p align="center">
  <a href="#about">About</a> |
  <a href="#whats-new-in-20">What's New in 2.0</a> |
  <a href="#features">Features</a> |
  <a href="#usage">Usage</a> |
  <a href="#tiktok-mode">TikTok Mode</a> |
  <a href="#technical-notes-en">Technical Notes</a> |
  <a href="#portugues">Português</a>
</p>

NoiseCloud 2.0 is a video-based data storage tool built around visual encoding. It converts files of any format into visual-noise frames, for later transport and recovery through `.mp4` video.

This release changes the main public engine: the standard flow now uses the `Weave 1.0` codec (`WEV1`) with data blocks and rescue frames, instead of the earlier public flow centered on per-frame Reed-Solomon. TikTok mode remains available, and the terminal experience is cleaner.

Author: **Lucas Ferraz**  
Language: **Go**  
License: **GPL-3.0**

---

## About

NoiseCloud 2.0 turns a regular file into an `.mp4` video containing visually encoded data. During encode, the file is compressed with Gzip, packed into `Weave 1.0` (`WEV1`) blocks, converted into high-contrast macropixels and rendered to video through FFmpeg. During decode, the video is extracted frame by frame, calibrated, reconstructed and decompressed, until the original file is recovered.

The project does not depend on a server, external account or database. The main workflow remains local and terminal-based.

---

## What's New in 2.0

- Standard public engine replaced by `Weave 1.0 / WEV1`
- Block protection using `16` data frames + `2` rescue frames
- More compact default preset at `640x360 @ 30 fps`
- TikTok preset kept at `1080x1920 @ 30 fps`
- Standard decode now checks for a `WEV1` trailer before frame reconstruction
- Cleaner terminal flow with FFmpeg noise removed from the main UX
- Revised interactive menu and status messages

Compared with the original `NoiseCloud Lite` release, this version focuses on:

- lower visual cost in standard mode
- simpler default encode/decode flow
- documentation aligned with the current engine
- clearer separation between standard and TikTok modes

---

## Features

### Encode and decode

- Encode files into `.mp4` videos
- Decode videos to recover the original file
- Automatic Gzip compression before encode
- Raw payload reconstruction before final decompression
- Validation of the recovered file before completing the operation

### Video and processing

- FFmpeg support
- Standard `weave` encode preset at `640x360`
- TikTok encode preset at `1080x1920`
- H.264 rendering with `faststart`
- Concurrent pipeline for rendering and reconstruction

### Resilience

- `WEV1` block packing with `16+2` layout
- High-contrast binary macropixels (`0/255`)
- Per-frame calibration bar
- Threshold autocalibration during decode
- Alternative alignment and reconstruction attempts in the decoder

> [!NOTE]
> TikTok mode remains experimental. It is kept because it survives resize and recompression better on vertical video, but video platforms are still hostile channels.

The executable should not be committed to the repository. Public binaries should be distributed as Release assets.

---

## Requirements

NoiseCloud 2.0 requires FFmpeg installed and available on the system.

> [!TIP]
> On Windows, open PowerShell as Administrator and install it with:
>
> ```powershell
> winget install -e --id Gyan.FFmpeg
> ```

---

## Usage

Run the program inside the folder containing the executable:

```powershell
.\ncc.exe
```

Or, if it is available in your `PATH`:

```bash
ncc
```

In the interactive menu, choose the desired action:

![Usage Demo](img/noisecloud.gif)

| Option | Purpose |
|---|---|
| `1` | Standard encode with the Weave engine |
| `2` | TikTok encode in vertical `9:16` video |
| `3` | Standard decode with the Weave engine |
| `4` | TikTok decode with dedicated grid/autocalibration |
| `5` | Exit |

Recommendation: keep the input file in the same folder as the executable, to simplify menu usage.

---

## TikTok Mode

TikTok mode uses a vertical preset designed to better survive resize and recompression.

Main parameters:

| Item | Value |
|---|---|
| Encode resolution | `1080x1920` |
| Aspect ratio | `9:16` |
| FPS | `30` |
| MacroSize | `45` during encode |
| Gray Levels | `2` |
| Calibration | dedicated top bar |

During decode, the reconstructor detects the real extracted video size, recalculates the grid, uses the calibration bar and tries alignment and threshold variations. This helps when the downloaded video comes back at another resolution, after upload and download.

---

<a id="technical-notes-en"></a>

## Technical Notes

- The `WEV1` format organizes payload into blocks with data frames and rescue frames.
- The default `weave` preset uses `640x360`, `MacroSize 8`, `30 fps` and `GrayLevels 2`.
- The TikTok preset uses `1080x1920`, `MacroSize 45`, `30 fps` and a dedicated vertical grid.
- The decoder attempts to read a `WEV1` trailer before falling back to visual frame reconstruction.
- The calibration bar helps the decoder estimate black and white levels after H.264 compression.
- NoiseCloud 2.0 is not encryption. For real secrecy, encrypt the file before encoding it.

---

## Project Structure

```text
cmd/cli/           interactive menu and encode/decode flow
internal/encoder/  frame rendering, FFmpeg pipeline and Weave
internal/decoder/  frame extraction, trailer and reconstruction
internal/weave/    WEV1 engine configuration and format
pkg/utils/         shared utilities
css/, js/, img/    presentation page and assets
```

## License

GPL-3.0. See [`LICENSE`](LICENSE).
