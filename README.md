---
type: Note
belongs_to: "[[ort]]"
---
# Ejemplo ecs + dynamo + amplify

## Proyecto de ejemplo: "Encuesta Rápida" (Poll App)

**Objetivo de aprendizaje:** construir y desplegar de punta a punta una aplicación con arquitectura desacoplada backend/frontend en AWS, usando contenedores (ECS/Fargate), una base de datos NoSQL administrada (DynamoDB) y hosting estático administrado (Amplify).

Este material está pensado para ser reproducido paso a paso en clase. La aplicación es intencionalmente simple (una sola pantalla, 3 endpoints) para que la complejidad esté puesta en la **arquitectura**, no en la lógica de negocio.

## Qué vamos a construir

**Encuesta Rápida**: una única pregunta con 3 opciones. Cualquier visitante puede votar y ve los resultados actualizados en tiempo real (al recargar). Un botón permite reiniciar la votación (útil para hacer demos en clase).

### Arquitectura general


```
                ┌──────────────────────┐
Usuario ──────▶ │       Frontend       |    
                | AWS Amplify Hosting  │ 
                │  (Angular, estático) |
                └──────────┬───────────┘
                           │ HTTPS (fetch/HttpClient)
                ┌──────────▼───────────┐
                │    Application Load  │
                │     Balancer (ALB)   │
                └──────────┬───────────┘
                           │
                ┌──────────▼───────────┐
                |       Backend        |
                │ ECS Fargate Service  │ 
                |    contenedor Go     | 
                └──────────┬───────────┘
                           │ AWS SDK v2 (DynamoDB)
                ┌──────────▼───────────┐
                │        Storage       │
                │    Tabla DynamoDB    │
                │     "PollOptions"    │
                └──────────────────────┘
```


- **Frontend**: Angular (standalone components), desplegado como sitio estático en **AWS Amplify Hosting**.
- **Backend**: API REST en **Go**, empaquetado en un contenedor Docker, desplegado en **ECS Fargate** detrás de un **Application Load Balancer**.
- **Persistencia**: **DynamoDB**, una tabla con una fila por opción de la encuesta.

### Modelo de datos (DynamoDB)

Tabla `PollOptions`, clave primaria simple (sin sort key):

| Atributo | Tipo | Descripción |
| --- | --- | --- |
| `OptionID` | String | Partition key. Ej: `"go"`, `"python"` |
| `OptionText` | String | Texto visible. Ej: `"Go"` |
| `Votes` | Number | Contador de votos |

La pregunta de la encuesta **no** se guarda en la base: se define como variable de entorno del backend (`POLL_QUESTION`). Esto simplifica el modelo y da pie a discutir en clase qué datos van en la base y cuáles en configuración.

### 1.3 API (3 endpoints)

| Método | Path | Descripción |
| --- | --- | --- |
| GET | `/api/poll` | Devuelve la pregunta y las opciones con sus votos actuales |
| POST | `/api/poll/vote` | Body `{"optionId": "go"}`. Incrementa el voto de una opción |
| POST | `/api/poll/reset` | Pone todos los contadores en 0 |

> Nota: usamos `GET /api/poll` también como *health check* del Load Balancer, así no necesitamos un cuarto endpoint dedicado.

## Prerrequisitos

- Cuenta de AWS. La guía está pensada para ser ejecutada en una cuenta de aws educate
- Go 1.25 o superior instalado.
- Node.js 18+ y Angular CLI (`npm install -g @angular/cli`).
- Docker instalado y corriendo.
- AWS CLI instalado y configurado (`aws configure`) — lo usamos únicamente para construir y subir la imagen a ECR; el resto de los pasos de AWS se hacen por consola.
- Una cuenta de GitHub (para conectar Amplify).

## Crear la tabla DynamoDB

1. Consola de AWS → **DynamoDB** → **Create table**.
2. Table name: `PollOptions`.
3. Partition key: `OptionID`, tipo **String**.
4. Dejar el resto en valores por defecto (On-demand / Pay-per-request está bien para la demo).
5. **Create table**.
6. Una vez creada, ir a la pestaña **Explore table items** → **Create item** y cargar 3 filas iniciales, por ejemplo:

| OptionID | OptionText | Votes |
| --- | --- | --- |
| `go` | `Go` | `0` |
| `python` | `Python` | `0` |
| `js` | `JavaScript` | `0` |

(Al crear cada ítem, agregar los atributos `OptionText` como String y `Votes` como Number).

## Backend en Go

El código del backend se encuentra en este repositorio:

[https://github.com/nfornaro/poll-backend](https://github.com/nfornaro/poll-backend)

**Puntos para discutir en clase:**

Breve introducción a DynamoDB y a las bases de datos clave-valor en general. Puntualmente sobre el código del ejemplo:

- Usamos `Scan` porque la tabla tiene solo 3-4 ítems. Buen disparador para hablar de por qué `Scan` es una mala idea en tablas grandes y cuándo usar `Query`.
- `ADD Votes :inc` es una actualización **atómica**: dos votos simultáneos no se pisan entre sí. Vale la pena compararlo con un `GET` + `PUT` (race condition).
- El endpoint de reset usa un loop de `UpdateItem`. Con más ítems convendría `BatchWriteItem`; queda como posible ejercicio.

### Prueba local (sin AWS todavía)

Si tenés credenciales de AWS configuradas localmente (`aws configure`) y ya creaste la tabla, podés probar el backend antes de tocar contenedores:

```shellscript
export DYNAMODB_TABLE=PollOptions
export POLL_QUESTION="¿Cuál es tu lenguaje de programación favorito?"
export AWS_REGION=us-east-1
go run main.go
```


```shellscript
curl http://localhost:8080/api/poll
curl -X POST http://localhost:8080/api/poll/vote -d '{"optionId":"go"}'
curl -X POST http://localhost:8080/api/poll/reset
```

### Dockerfile (build multi-stage)

```dockerfile
# Etapa 1: compilación
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server .

# Etapa 2: imagen final, liviana
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
ENTRYPOINT ["./server"]
```


> Importante: `ca-certificates` es necesario porque el SDK de AWS habla HTTPS con DynamoDB. Sin este paquete, en `alpine` las llamadas fallan por certificados no confiables — es un error clásico y vale la pena mencionarlo en clase.

### Crear el repositorio en ECR y subir la imagen

1. **ECR** → **Repositories** → **Create repository**.
2. Nombre: `poll-backend`. Dejar el resto por defecto. **Create**.
3. Construir y subir la imagen (esto sí requiere terminal/CLI, es inevitable para trabajar con Docker). **Importante** reemplazar `857344665088` por el id de la cuenta de aws:

```shellscript
  aws ecr get-login-password --region us-east-1 \
     | docker login --username AWS --password-stdin 857344665088.dkr.ecr.us-east-1.amazonaws.com

docker buildx build --platform linux/amd64 -t poll-backend . --load

docker tag poll-backend:latest \
     857344665088.dkr.ecr.us-east-1.amazonaws.com/poll-backend:latest

docker push 857344665088.dkr.ecr.us-east-1.amazonaws.com/poll-backend:latest
```


### Crear el cluster de ECS

ECS → Clusters → Create new cluster.

El cluster debe ser del tipo Fargate Serverless.

### Crear la Task Definition

ECS → Task definitions → Create new task definition.

- Nombre de familia: poll-backend-task.
- Launch type: Fargate.
- Task size: 0.5 GB de memoria, 0.25 vCPU (alcanza de sobra para la demo).
- Task Role: el del lab.
- Container:
  - Name: poll-backend.
  - Image URI: Seleccionar la imagen creada antes
  - Port mappings: 8080, protocolo TCP.

Environment variables:

- DYNAMODB_TABLE	PollOptions
- POLL_QUESTION	¿Cuál es tu lenguaje de programación favorito?
- CORS*ALLOWED*ORIGIN	\* (lo ajustamos después de desplegar el frontend)

Logging: dejar Use log collection activado (crea el grupo de CloudWatch automáticamente).

Create.

### Crear el servicio (con Load Balancer)

1. Entrar al cluster `poll-cluster` → pestaña **Services** → **Create**.
2. Task definition: `poll-backend-task` (última revisión).
3. Service name: `poll-backend-service`.
4. Desired tasks: `1`.
5. Networking:
   - VPC: la default.
   - Subnets: seleccionar las subnets públicas.
   - Security group: **usar uno existente** → debe tener abiertos los puertos de entrada 80 (HTTP) y 443 (HTTPS).
   - Public IP: **Turned on** (necesario para que la tarea pueda bajar la imagen y hablar con DynamoDB sin NAT Gateway).
6. Load balancing:
   - Tipo: **Application Load Balancer**.
   - Crear uno nuevo: nombre `poll-alb`.
   - Security group del ALB:  **usar uno existente** → debe tener abiertos los puertos de entrada 80 (HTTP) y 443 (HTTPS).
   - Listener: puerto 80.
   - Target group: crear uno nuevo, **health check path** = `/api/poll`.
7. **Create**.
8. Esperar a que el servicio quede en estado **Running** (puede tardar 1-2 minutos) y que el *target* del target group pase a **healthy**.

### Verificar el backend

1. Ir a **EC2 → Load Balancers**, copiar el **DNS name** de `poll-alb` (algo como [`poll-alb-123456.us-east-1.elb.amazonaws.com`](http://poll-alb-123456.us-east-1.elb.amazonaws.com)).
2. Probar desde la terminal o el navegador:


```shellscript
   curl http://poll-alb-123456.us-east-1.elb.amazonaws.com/api/poll
```


Debería devolver el JSON con la pregunta y las 3 opciones en 0 votos.

## Frontend en Angular

### Codigo Angular

El codigo de la aplicación angular se puede encontrar en este repositorio:

[https://github.com/nfornaro/poll-frontend](https://github.com/nfornaro/poll-frontend)

### Variables de entorno

Se debe cambiar en los archivos `/src/environments` la url del backend.

```javascript
export const environment = {
  production: false,
  apiUrl: 'http://localhost:8080'
};
```


También podemos probar local contra el backend en aws.

```javascript
export const environment = {
  production: true,
  apiUrl: 'http://test-lb-587285437.us-east-1.elb.amazonaws.com'
};
```

## Amplify

Archivo de build para Amplify (ya está en el repo):

```yaml
version: 1
frontend:
  phases:
    preBuild:
      commands:
        - npm ci
    build:
      commands:
        - npm run build -- --configuration production
  artifacts:
    baseDirectory: dist/poll-frontend/browser
    files:
      - '**/*'
  cache:
    paths:
      - node_modules/**/*
```


### Crear la app en Amplify

1. Consola de AWS → **AWS Amplify** → **New app** → **Host web app**.
2. Elegir **GitHub**, autorizar el acceso y seleccionar el repositorio `poll-frontend` y la rama `main`.
3. Amplify detecta el `amplify.yml` automáticamente (o lo pega manualmente si prefieren mostrarlo en clase).
4. Nombre de la app: `poll-frontend`.
5. **Save and deploy**.
6. Esperar a que las 4 fases (Provision, Build, Deploy, Verify) terminen en verde.

### (Recomendado) Restringir CORS a la URL real

Buena oportunidad para hablar de seguridad: dejar \* en CORS está bien para la demo, pero en un caso real conviene restringirlo.

Copiar la URL de Amplify.

ECS → Task Definitions → poll-backend-task → Create new revision.

Cambiar CORS*ALLOWED*ORIGIN al valor [https://main.dABCDEFGH123.amplifyapp.com](https://main.dABCDEFGH123.amplifyapp.com). (reemplazar por la url real dada por amplify)

Create.

Ir al servicio poll-backend-service → Update service → seleccionar la nueva revisión de la task definition → Update.

### Problema de mixed content

Amplify expone mediante HTTPS y al browser no le gusta que el js del sitio vaya contra algo HTTP. Hay varias posibles soluciones para exponer el backend mediante HTTTPS, pero para evitar costos, usamos un certificado *self signed*.

#### 1. Generar el certificado autofirmado (en tu máquina)

```shellscript
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout private-key.pem \
  -out certificate.pem \
  -subj "/CN=poll-lb.local"
```


#### Importar el certificado a ACM

1. Consola de AWS → **Certificate Manager** → **Import a certificate**.
2. Pegar el contenido de `certificate.pem` en **Certificate body**.
3. Pegar el contenido de `private-key.pem` en **Certificate private key**.
4. **Import**.

#### Agregar el listener HTTPS al ALB

1. **EC2** → **Load Balancers** → `poll-lb` → pestaña **Listeners** → **Add listener**.
2. Protocol: **HTTPS**, puerto **443**.
3. Default action: forward al mismo target group que ya tenías.
4. **Security policy**: dejar la recomendada por defecto.
5. **Default SSL/TLS certificate**: elegir **From ACM** y seleccionar el certificado que acabás de importar.
6. **Add**.

#### Habilitar el puerto 443 en el Security Group del ALB

**EC2 → Security Groups** → `poll-alb-sg` → **Inbound rules** → **Add rule** → HTTPS (443) desde `0.0.0.0/0`.

#### Actualizar el front end

```typescript
export const environment = {
  production: true,
  apiUrl: 'https://poll-lb-XXXXXXX.us-east-1.elb.amazonaws.com'
};
```

#### Paso extra necesario por ser autofirmado (importante para la clase)

Como el certificado no es de una entidad confiable, el navegador va a bloquear silenciosamente el fetch con un error de certificado — a diferencia de una navegación normal, fetch/XHR no muestra un botón de "continuar de todas formas". Hay que "enseñarle" al navegador a confiar en ese certificado primero:

Abrir en una pestaña nueva, directamente: [https://poll-lb-XXXXXXX.us-east-1.elb.amazonaws.com/api/poll](https://poll-lb-XXXXXXX.us-east-1.elb.amazonaws.com/api/poll).
El navegador va a mostrar una advertencia ("La conexión no es privada" / NET::ERR*CERT*AUTHORITY_INVALID).
Click en Avanzado → Continuar de todas formas.
Una vez que eso carga el JSON, recién ahí volver a la app de Amplify — el navegador ya "recuerda" la excepción para ese origen en esa sesión.

Es un paso manual medio incómodo, pero típico de labs sin dominio propio — vale la pena explicárselo a los alumnos como parte de la clase (por qué existe, qué reemplazaría en un entorno real: un certificado público con dominio válido).

# Resumen de aws

Acá va un resumen conceptual de cada pieza, en el orden en que las creamos:

**DynamoDB — Tabla** `PollOptions
`Base de datos NoSQL administrada. No es un servidor que vos gestionás: es un servicio donde solo definís la estructura de la tabla (partition key) y AWS se encarga de escalado, disponibilidad y replicación. Acá guarda cada fila (opción de la encuesta + contador de votos).

**IAM Role (Task Role / Task Execution Role /** `LabRole`**)**
Un rol es una **identidad sin credenciales fijas**: en vez de poner un usuario/contraseña o access keys en el código, la tarea de ECS "asume" el rol y recibe credenciales temporales automáticamente. El **Task Role** define qué puede hacer *tu código*(hablar con DynamoDB); el **Task Execution Role** define qué puede hacer *el agente de ECS* (bajar la imagen de ECR, mandar logs a CloudWatch). Son permisos para actuar sobre otros servicios, no accesos de usuario humano. En este ejemplo usamos en todos los casos el LabRole predefinido por aws academy.

**ECR (Elastic Container Registry) — Repositorio** `poll-backend
`Es un registro privado de imágenes Docker, equivalente a un Docker Hub privado dentro de tu cuenta de AWS. Ahí subís (`push`) la imagen que construiste localmente, y ECS la descarga (`pull`) desde ahí para correrla.

**Security Group (**`poll-alb-sg`**,** `poll-ecs-sg`**)**
Es un firewall virtual a nivel de instancia/tarea. No filtra por IP de origen únicamente: podés decir "permití tráfico que venga de *este otro* security group", que es justo lo que hacemos para que solo el ALB pueda hablarle al contenedor, y nadie más directamente.

**ECS Cluster (**`poll-cluster`**)**
Es una agrupación lógica de recursos de cómputo donde corren tus contenedores. Con Fargate no administrás servidores: el cluster es más un "namespace" organizativo que un conjunto de máquinas.

**Task Definition (**`poll-backend-task`**)**
Es la **plantilla** que describe cómo correr tu contenedor: qué imagen usar, cuánta CPU/memoria, qué puerto expone, qué variables de entorno tiene, y qué roles de IAM usar. Es análoga a un `docker-compose.yml`, pero versionada (cada cambio genera una nueva "revisión").

**ECS Service (**`poll-backend-service`**)**
Es el objeto que mantiene viva una cantidad deseada de tareas basadas en una Task Definition. Si una tarea se cae, el service lanza otra automáticamente. También es el que se integra con el Load Balancer para registrar/desregistrar tareas.

**Task (instancia en ejecución)**
Es la ejecución concreta de una Task Definition — el contenedor corriendo de verdad, con una IP asignada. El Service crea y destruye Tasks según haga falta.

**Application Load Balancer (ALB,** `poll-lb`**)**
Recibe el tráfico HTTP público (puerto 80) y lo reenvía a las tareas sanas. Permite que el frontend siempre pegue a una misma URL/DNS estable, aunque las tareas de atrás cambien de IP constantemente (por reinicios, deploys, escalado).

**Target Group**
Es la lista de "destinos" (tareas) a las que el ALB puede enviar tráfico, junto con la configuración del *health check* (qué endpoint pinguear para saber si un destino está sano). El ALB nunca habla directo con una tarea sin pasar por acá.

**AWS Amplify Hosting (**`poll-frontend`**)**
Es un servicio de hosting estático con CI/CD integrado: al conectarlo a un repo de GitHub, cada push dispara automáticamente build + deploy del frontend Angular compilado, sirviéndolo por HTTPS desde un CDN, sin que vos administres ningún servidor.

## Limpieza

Al terminar el trabajo es recomendable eliminar todos los elementos de aws creados para evitar costos. El orden sugerido de borrado es:

- sitio de amplify
- servicio de ecs (utilizar la opción de forzar borrado)
- cluster de ecs
- dynamo
- repo ecr
- load balancer / target group
- imagen de github local
- certificado
