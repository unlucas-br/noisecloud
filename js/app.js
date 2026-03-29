const documentModel = {
    title: "NoiseCloud Lite",
    introduction: "Este documento apresenta uma análise profunda sobre as etapas de compressão, codificação (encode) e decodificação (decode) de dados operadas pelo sistema <strong>NoiseCloud</strong>. A arquitetura foi avaliada identificando-se as principais estruturas no código Go que garantem a conversão segura de arquivos binários em vídeo esteganográfico focado em armazenamento em plataformas de nuvem.",

    benchmark: {
        title: "1. Resultados Práticos de Benchmark (Encode & Decode)",
        description: "Para comprovar a eficiência das implementações avaliadas teóricamente a seguir, foi injetado um fluxo misto de dados preenchido com bytes estáticos de alta replicação misturados com entropia (ruído via `urandom` local) totalizando em 2.00 MB. Os testes foram mensurados em pipeline isolado em hardware utilizando aceleração de vídeo híbrida e paralelismo multi-threading (10 threads base):",
        metrics: [
            { label: "Tamanho do Arquivo Original", value: "2.00 MB", obs: "Carga mista usada como Input no software." },
            { label: "Tamanho Pós-Compressão GZip", value: "1.00 MB (1.051.187 bytes)", obs: "Aproximadamente 50% de redução (depende fortemente do nível de entropia e repetição dos dados brutos)." },
            { label: "Quadros (Frames) Ocupados", value: "3865 frames", obs: "Total de quadros gerados pelo modelo HD preset para empacotar o payload comprimido de 1 MB." },
            { label: "Tempo Total de Encode", value: "26.87 segundos", obs: "Velocidade alcançada através de injeção paralela (Goroutines) + Hardware NVENC GPU, resultando em ~143 fps." },
            { label: "Tamanho Final do Vídeo MP4", value: "14.36 MB", obs: "Expansão física natural do espaço de amostragem por usar pixels aglutinados (MacroPixeis 16x16) para resistir aos processos Lossy das clouds." },
            { label: "Tempo Total de Decode", value: "46.32 segundos", obs: "Decodificação sofre com o bypass necessário ao parser do FFmpeg, varredura matricial e calibração para extrair diferencial por Macroblock em CPU limitando a performance ao processo analítico pixel-a-pixel." },
            { label: "Integridade da Reconstrução", value: "100% Validada", obs: "Após a remontagem final e descompressão Gzip, os 2MB devolvidos ao disco eram matemática e criptograficamente idênticos ao arquivo inserido via input. Nenhuma margem de tolerância falhou." }
        ]
    },

    sections: [
        {
            title: "2. O Modelo de Compressão de Dados",
            intro: "O processo de inserção de dados restringe-se pela capacidade visual da rede de <em>Macro Pixels</em>. Portanto, antes de realizar a conversão visual, o sistema foca em redução volumétrica pura.",
            reference: { file: "cmd/cli/main.go", func: "compressData(data []byte) -> Invocada em runEncode()" },
            content: "O sistema adota <strong>Gzip</strong> em memória utilizando a biblioteca padrão (<code>compress/gzip</code>) do Go. O payload binário inicial é lido integralmente para a RAM, passado pelo compressor e repassado para o pipeline como um slice de bytes (<code>[]byte</code>) reduzido. Esta compressão <em>lossless</em> atua diretamente sobre a redundância intrínseca dos bytes de entrada.",
            highlight: {
                title: "Impacto nos Resultados:",
                text: "A aplicação do Gzip diminui drasticamente o número de quadros gerados no vídeo final (proporcional à taxa de compressibilidade do arquivo de input). Como avaliado no benchmark, arquivos compressíveis reduzem 50% ou mais o volume de bytes que precisam se transformar em matriz de pixels, diminuindo o tempo de renderização em cascata pela mesma escala."
            }
        },
        {
            title: "3. Engenharia de Encode: Do Binário ao Frame",
            intro: "A etapa de Encode converte pacotes comprimidos em matrizes matemáticas de cores (Macro Pixels) prontas para o encoder em vídeo (FFmpeg).",
            subsections: [
                {
                    title: "3.1. Injeção de Redundância e Correção de Erros (ECC)",
                    reference: { file: "internal/encoder/reed_solomon.go", func: "ECCConfig{DataShards: 16, ParityShards: 8}" },
                    content: "Antecipando a corrupção de pixels introduzida por compressores de vídeo em nuvem (ex: processamento VP9/H.264 do YouTube), o sistema fatia os dados do Gzip sob o algoritmo <strong>Reed-Solomon</strong>. Com 16 shards de dados e 8 de paridade, a engenharia garante que o bloco binário do frame será recuperável mesmo que até 33% dos pixels no vídeo se degradem para um limiar ilegível durante transmissões ou compressões agressivas de downscale no player.",
                    list: [
                        "O Header identificador <strong><code>NCC3</code></strong> contendo o ID do frame atual e tamanho total é prefixado logicamente e também coberto pelo ECC."
                    ]
                },
                {
                    title: "3.2. O Mapeamento de Pixels e Malha Gráfica",
                    reference: { file: "internal/encoder/framer.go & macro_pixel.go", func: "FrameConfig e interface ByteToGray()" },
                    content: "Ao invés de pintar pixel a pixel isoladamente (o que não suportaria nem compressões de 2% de reescala de bitrate), o sistema unifica \"quadrados espaciais\" grandes (ex: 16x16 pixels ou 24x24 pixels) denominados <strong>MacroPixels</strong>. No modo de conversão, os bytes não são lançados sob canais RGB complexos para driblar artefatos cromáticos do espaço YUV 4:2:0:",
                    list: [
                        "<strong>Sistema Binário (2 Gray Levels):</strong> Bits de informação são lidos (1 ou 0) e renderizados como blocos cinza-claros (nível 224) ou cinza-escuros (nível 32). Extrema robustez contra ruídos de compactação de alto nível limitadores de luminância;",
                        "<strong>Camada YUV 4:2:0:</strong> Para preservar uma compressão de cores estável, os blocos são alimentados unicamente por Luma (escuro para claro), mantendo as camadas <strong>U</strong> e <strong>V</strong> em \"128\" (cinza neutro exato no pipeline YUV -> RGB)."
                    ]
                },
                {
                    title: "3.3. Integração com FFmpeg e GPU",
                    reference: { file: "internal/encoder/video.go", func: "StartFFmpegPipe() e Módulo Concorrente Concurrent" },
                    content: "O processamento final não usa gravação em disco transiente quadro-a-quadro para evitar exaustão de I/O em máquinas convencionais. A pipeline é embutida em Goroutines (baseado nos cores livres da CPU) injetando o frame visual diretamente no FFmpeg através de <code>stdin</code> via Pipe. Adicionalmente há algoritmos de detecção de infraestruturas via <code>VerifyGPU()</code> invocando automaticamente engines <code>h264_nvenc</code>, <code>h264_qsv</code> ou <code>h264_amf</code> para escalar processamento maciço em hardware local em até 140+ FPS.",
                    list: []
                }
            ]
        },
        {
            title: "4. Engenharia de Decode: Autocalibração e Extração",
            intro: "A fase complexa ocorre no momento do <em>Decode</em>, pois agora o sistema lida com dados que sofreram entropia visual nas cloud infrastructures.",
            subsections: [
                {
                    title: "4.1. Extração Limpa e Snapshots Estáticos",
                    reference: { file: "internal/decoder/extractor.go" },
                    content: "Através do CLI (<code>-hwaccel auto -vsync 0</code>), são exigidos frames YUV decodificados perfeitamente retornados como <code>.png</code> localmente num temp dir. Ao abstrair o conceito de fluidez para apenas imagens singulares, removemos gargalos de sincronia de reprodução no processo reverso.",
                    list: []
                },
                {
                    title: "4.2. Compensação de Iluminação (Limiares Dinâmicos)",
                    reference: { file: "internal/decoder/reconstructor.go", func: "calibrateFrame(), calibrateLevels()" },
                    content: "Arquivos em nuvem frequentemente sofrem <em>brightness shifting</em> (escurecimento sutil pelo player de vídeo para poupar banda em pretos). A barra visualmente injetada pelos Encoders chamada de <strong>CalibrationHeight</strong> (Faixa de pixels alternados branco/preto disposta no topo da resolução do output MP4) é analisada neste passo.",
                    list: [
                        "O algoritmo processa a média luminosa do bloco de referência White/Black e gera um novo <em>Threshold</em> (limite para categorizar binários). Desse modo, um byte que entrou como \"224 (quase branco)\", mesmo que o YouTube degradei para \"150 (cinza médio)\", ainda será legível ao compará-lo com o Threshold dinamicamente rebaixado."
                    ]
                },
                {
                    title: "4.3. Algoritmo de Luma Diferencial",
                    reference: { file: "internal/decoder/reconstructor.go", func: "extractDifferentialMacroPixel()" },
                    content: "Para casos complexos de filtragens severas ocorridas na rede, a varredura convencional via Threshold pode falhar e descartar muitos frames. Neste cenário, o motor roda o Fallback Diferencial. Avalia-se o lado direito versus esquerdo dentro do <em>Macro Pixel individualmente</em>, cortando problemas de ruídos espaciais na vertical (ghosting artefacts das árvores de decisão P/B e Macroblocos H.264 do transcodificador de Nuvem).",
                    list: []
                },
                {
                    title: "4.4. Reconstrução ECC e GZip",
                    reference: null,
                    content: "Em posse dos bits isolados pelas camadas de limiarização, o bloco é particionado novamente. Os defeitos não resolvidos visualmente são passados à rotina de Reed-Solomon que, lendo através de <code>ecc.Reconstruct()</code>, forçará a álgebra linear galoariana para suprir a matemática faltante dos shards do arquivo.<br><br>Recuperado o frame, ordenam-se as peças através do índice do protocolo NCC3 até a reconstituição do <em>raw payload</em> integral. Por fim, o sub-motor do Gzip na main descompacta e devolve a integridade exata <em>byte-to-byte</em> do arquivo original localmente.",
                    list: []
                }
            ]
        }
    ],

    conclusion: "A arquitetura NoiseCloud adota práticas avançadas de resistência à codificação com perdas, transformando as limitações dos codecs YUV e compressão lossy em um campo probabilístico corrigível através do ECC agressivo e da re-calibração nativa em Go-lang. A validação de 100% de integridade nos experimentos com hardwares comprova a sinergia entre o software de empacotamento MacroPixel e a lógica final Gzip."
};

class DocumentController {
    constructor(model, containerId) {
        this.model = model;
        this.container = document.getElementById(containerId);
    }

    render() {
        let html = `<h1>${this.model.title}</h1>`;
        html += `<p>${this.model.introduction}</p>`;

        html += `<h2>${this.model.benchmark.title}</h2>`;
        html += `<p>${this.model.benchmark.description}</p>`;
        html += `<table><thead><tr><th>Métrica Medida</th><th>Resultado Real</th><th>Observação</th></tr></thead><tbody>`;

        this.model.benchmark.metrics.forEach(m => {
            html += `<tr>
                        <td>${m.label}</td>
                        <td>${m.value}</td>
                        <td>${m.obs}</td>
                     </tr>`;
        });
        html += `</tbody></table>`;

        this.model.sections.forEach(section => {
            html += `<h2>${section.title}</h2>`;
            if (section.intro) html += `<p>${section.intro}</p>`;

            if (section.content && !section.subsections) {
                if (section.reference) {
                    html += `<div class="reference">Arquivo: <code>${section.reference.file}</code>`;
                    if (section.reference.func) html += `<br>Função: <code>${section.reference.func}</code>`;
                    html += `</div>`;
                }
                html += `<p>${section.content}</p>`;
                if (section.highlight) {
                    html += `<div class="highlight-box">
                                <div class="highlight-title">${section.highlight.title}</div>
                                <p>${section.highlight.text}</p>
                             </div>`;
                }
            }

            if (section.subsections) {
                section.subsections.forEach(sub => {
                    html += `<h3>${sub.title}</h3>`;

                    if (sub.reference) {
                        html += `<div class="reference">Arquivo: <code>${sub.reference.file}</code>`;
                        if (sub.reference.func) html += `<br>Configuração/Função: <code>${sub.reference.func}</code>`;
                        html += `</div>`;
                    }

                    html += `<p>${sub.content}</p>`;

                    if (sub.list && sub.list.length > 0) {
                        html += `<ul>`;
                        sub.list.forEach(li => {
                            html += `<li>${li}</li>`;
                        });
                        html += `</ul>`;
                    }
                });
            }
        });

        html += `<hr style="margin-top: 40px; border: 0; border-top: 1px solid var(--border-color);">`;
        html += `<p style="text-align: center; color: #6b7280; font-size: 0.9em;"><strong>Conclusão Técnica:</strong> ${this.model.conclusion}</p>`;

        this.container.innerHTML = html;
    }
}

window.onload = () => {
    const app = new DocumentController(documentModel, "app-view");
    app.render();
};

window.showSection = function (section) {
    const articleView = document.getElementById('article-view');
    const appView = document.getElementById('app-view');
    const btnArticle = document.getElementById('btn-article');
    const btnReport = document.getElementById('btn-report');

    if (section === 'article') {
        articleView.style.display = 'block';
        appView.style.display = 'none';
        btnArticle.classList.add('active');
        btnReport.classList.remove('active');
    } else {
        articleView.style.display = 'none';
        appView.style.display = 'block';
        btnArticle.classList.remove('active');
        btnReport.classList.add('active');
    }
};
