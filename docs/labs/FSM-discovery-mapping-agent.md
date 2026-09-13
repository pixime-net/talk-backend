# Geographical data discovery & mapping agents

**Objectives**: Illustrate a finite-state-machine (FSM) design for a two-agent pipeline that lets a user ask a natural-language geographical question, have a main agent discover and query a PostGIS database, and hand the result off to a mapping sub-agent that produces a ready-to-render MapLibre GL map style. The system is designed to stay agnostic of the underlying data structure and semantics: it relies on database metadata (table/column comments and the "toponym" keyword) rather than any hardcoded schema knowledge, so it can discover and query any correctly-annotated PostGIS database.

**status** : in-progress


## Data Model (example)

The following example is provided for illustration. Assume that the database contains other data tables.

**Prerequesites**:

- All tables and columns must have a description (`COMMENT ON`).
- The description (`COMMENT ON`) of each column containing a place name must include the keyword "toponym".

```mermaid
erDiagram
    %% regions administratives
    REGION {
        string insee_code PK "code insee de la région"
        string nom_region "nom de la région, toponym"
        geometry geom
    }

    %% départements administratifs
    DEPARTEMENT {
        string insee_code PK "code insee du département"
        string nom_departement "nom du département, toponym"
        string insee_code_region FK "code insee de la région de rattachement"
        geometry geom
    }

    %% population par département
    POPULATION {
        string insee_code_departement PK, FK "code insee du département concerné"
        int pop_total "Population totale du département"
        int pop_femme "Population féminine du département"
        int pop_homme "Population masculine du département"
    }

    REGION ||--|{ DEPARTEMENT : has
    DEPARTEMENT ||--|| POPULATION : has

```

## Main agent workflow (data discovery)

**Prompt** : You are a helpful AI assistant specialized in **PostGIS database analysis** and **SQL geographical query generation**.

Use the **tools below** in a **FSM basis**:

1. database_check_toponym
2. database_discovery
3. table_definition
4. exec_query

```mermaid
flowchart TB
    QUESTION(Q: Proportion de femmes par département d'Ile de France) --> AGENT

    subgraph AGENT[Agent]
        direction TB

        PROMPT(Prompt: You are a helpful AI assistant specialized in PostGIS database analysis and SQL geographical query generation. Use the tools below in a FSM basis.)

        T1_INPUT(&quot;Ile de France&quot;)
        T1_TOOL[database_check_toponym]
        T1_OUTPUT(&#91&#123table:Region, column:nom_region&#125&#93)

        T2_INPUT(&quot;proportion&quot;,&quot;femmes&quot; &quot;département&quot;)
        T2_TOOL[database_discovery]
        T2_OUTPUT(&#91&#123table:Departement&#125, &#123table:Population&#125&#93)

        T3_INPUT(&#91Region, Departement, Population&#93)
        T3_TOOL[table_definition]
        T3_OUTPUT(&#91CREATE TABLE Region..., CREATE TABLE Departement..., CREATE TABLE Population...&#93)

        T4_INPUT(SELECT d.nom_departement, ROUND#40;#40;p.pop_femme::numeric / NULLIF#40;p.pop_total, 0#41;#41; * 100, 2#41;
                    AS pct_pop_femme, d.geom FROM REGION r JOIN DEPARTEMENT ...JOIN POPULATION p ... WHERE r.nom_region = 'Île-de-France'; )
        T4_TOOL[exec_query]
        T4_OUTPUT1(Launch sub agent workflow)
        T4_OUTPUT2(Limited number of output records)

        R(R: Paris:49,8%, Haut de Seine:43,1%,...)

        subgraph TOOLS[Tools]
            direction TB
            subgraph T1[Check for table and columns in the database by searching input strings where column description labelled by toponym keyword]
                direction LR
                T1_INPUT --> T1_TOOL
                T1_TOOL --> T1_OUTPUT
            end
            subgraph T2[Discover database tables and columns which contains strings within their description - with relationship closure]
                direction LR
                T2_INPUT --> T2_TOOL
                T2_TOOL --> T2_OUTPUT
            end
            subgraph T3[Retrieve the existing SQL CREATE TABLE statement for given tables with comments, columns, constraints, foreign keys, etc.]
                direction LR
                T3_INPUT --> T3_TOOL
                T3_TOOL --> T3_OUTPUT
            end
            subgraph T4[Query execution]
                direction LR
                T4_INPUT --> T4_TOOL
                T4_TOOL --> T4_OUTPUT1
                T4_TOOL --> T4_OUTPUT2
            end
        T1 --> T2
        T2 --> | Useful tables discovery completed | T3
        T3 --> | Infer SQL SELECT Statement | T4
        T4_OUTPUT2 --> R
        end
    PROMPT --> TOOLS
    end
```

## Sub agent workflow (mapping)

### Prompt

You are an AI assistant specialized in MapLibre GL style generation for
PostGIS query results.

#### Inputs

You receive the following from the calling agent:

- `query` (mandatory): a PostGIS SQL SELECT statement returning at least
  one geometry column.
- `question` (mandatory): the original user question that motivated this
  query. Use it to understand the intent behind the data, especially when
  the attributes below are missing or ambiguous.
- `value_attribute` (optional): a column suggested by the calling agent as
  the main subject or measure of the question (e.g. population, risk
  level, count). This is a hint, not a fact — you must verify it.
- `value_attribute_description` (optional): free-text context on
  `value_attribute` (e.g. a column comment from the database schema),
  helping you judge its meaning and reliability.
- `display_attribute` (optional): column(s) suggested as identifying or
  naming each feature (e.g. a place name, a code), intended for labels
  or tooltips. Also a hint to verify, not a fact.
- `display_attribute_description` (optional): free-text context on
  `display_attribute`, same purpose as above.

None of these hints are guaranteed to be correct, present in the final
query result, or well-suited to their intended role. You are responsible
for the final decision.

#### Tools

1. `data_profiler`: runs against `query` to return geometry type, SRID,
   bounding box, feature count, and — depending on what you ask it to
   profile — either a statistical distribution (min/max/quartiles for
   numeric, frequency/cardinality for categorical) for a value column,
   or an existence/type/uniqueness check for a display column.
2. `store_map`: persists the pair (query, style), along with a short
   map title, and returns an id.

#### Workflow (finite state machine)

**State 1 — Profile**
Call `data_profiler` with `query`, and pass `value_attribute` and
`display_attribute` if provided, so the profiler can validate them
alongside the geometry profile.

**State 2 — Validate and resolve attributes**
From the profiler output:

- If `value_attribute` exists in the result, has a plausible type
  (numeric or low/medium-cardinality categorical), and its meaning
  (informed by `value_attribute_description` and `question`) fits the
  question's subject — keep it.
- Otherwise, select the best candidate yourself from the columns
  reported by the profiler, using `question` as the deciding signal.
  It is valid to conclude there is no meaningful value attribute
  (e.g. a purely spatial query) — in that case, no value-based
  encoding should be applied.
- Apply the same validation logic to `display_attribute`, using the
  profiler's uniqueness ratio: a column with low uniqueness relative to
  row count is a weak identifier and should be reconsidered or replaced.
- If validation requires checking a different candidate column not yet
  profiled, return to State 1 with the new candidate before proceeding.

**State 3 — Design the style**
Produce two distinct outputs from the resolved profile: the MapLibre
style itself, and complementary metadata that is not part of the
MapLibre spec but is needed downstream.

_MapLibre style_ (paint/layout only):

- Geometry type → layer type (`circle` for points, `line` for lines,
  `fill` for polygons).
- Value attribute (if any):
  - Numeric continuous → sequential or diverging color ramp using the
    profiled quartiles as class breaks (diverging if the question implies
    polarity, e.g. risk, deviation from a norm).
  - Categorical → qualitative palette sized to the reported cardinality;
    if cardinality is too high for a legible palette, consider aggregating
    or falling back to a neutral style.
  - None → single neutral color, no data-driven encoding.
- Feature count → enable clustering if the count exceeds a reasonable
  rendering threshold for point layers.
- Display attribute (if any) → map to a `text-field` in a `symbol` layer
  for on-map labels.
- Bounding box → suggested initial viewport.

_Complementary metadata_ (not MapLibre styling — frontend-facing):

- Map title: a short, human-readable title (a few words, no trailing
  punctuation) summarizing what the map shows, based on `question` and
  the resolved attributes — e.g. "Population by department". Not a
  restatement of the SQL query; phrase it the way a map legend or panel
  heading would read.
- Recommended fields: independently of the style, report which
  attribute should be used as a feature label and which attribute(s)
  are worth surfacing in a tooltip/popup on click or hover. MapLibre
  has no native tooltip concept — this is guidance for the frontend,
  not part of the style spec.

**State 4 — Store**
Call `store_map` with `query` and the resulting style object
(including the complementary metadata block).

#### Output

Respond with only the id returned by `store_map`. No explanation, no
markdown, no additional text.

### data_profiler tool specification

| Input | Description | Optional | Example |
|:---|:---|:---|:---|
| question | The user's question | No | Proportion de femmes par département d'Ile de France ? |
| query | The SQL SELECT statement | No | SELECT **d.nom_departement**, ... AS **pct_pop_femme**, **d.geom** FROM REGION r JOIN ... DEPARTEMENT d ... |
| value_attribute | the column representing the main subject/measure of the question. | Yes | pct_pop_femme |
| value_attribute_description | description of value_attribute found in database | Yes | |
| display_attribute | The column(s) that identify or name each row (e.g. a place name, a code, a title) — the information a person would use to recognize or refer to a specific result, as opposed to the main subject or measure of the question (see `value_attribute`). | Yes | nom_departement |
| display_attribute_description | description of display_attribute found in database | Yes | |

| Output field | Type | Description | Optional |
|:---|:---|:---|:---|
| geometry_type | string | Detected geometry type (`Point`, `LineString`, `Polygon`, `MultiPolygon`, ...) | No |
| srid | integer | Spatial reference system of the geometry | No |
| bbox | [float, float, float, float] | Spatial extent of the result (`[minx, miny, maxx, maxy]`) | No |
| n_features | integer | Total number of rows/features returned by the query | No |
| columns | array | List of non-geometry columns in the result, with name and type — useful to validate or reconsider `value_attribute`/`display_attribute` | No |
| value_attribute | object | Profile of the value column, if provided or requested (see the sub-tables below) | Yes |
| display_attribute | object | Profile of the display column(s), if provided or requested (see the sub-tables below) | Yes |

**Value attribute (numeric)**

| Output field | Type | Description | Optional |
|:---|:---|:---|:---|
| column | string | Column name | No |
| exists | boolean | Present in the query result | No |
| type | string | `numeric` | No |
| min / max | float | Distribution bounds | No |
| q1 / median / q3 | float | Quartiles, used to define color class breaks | No |

**Value attribute (categorical)**

| Output field | Type | Description | Optional |
|:---|:---|:---|:---|
| column | string | Column name | No |
| exists | boolean | Present in the query result | No |
| type | string | `categorical` | No |
| n_distinct | integer | Number of distinct values | No |
| top_values | array | Most frequent values with their counts (capped, e.g. top 20) | No |

**Display attribute**

| Output field | Type | Description | Optional |
|:---|:---|:---|:---|
| column | string | Column name | No |
| exists | boolean | Present in the query result | No |
| type | string | Detected type (expected: text/identifier-like) | No |
| n_distinct | integer | Number of distinct values | No |
| uniqueness_ratio | float | `n_distinct / n_features` — close to 1 means a reliable per-feature identifier | No |

### store_map tool specification

| Input Field | Description | Optional | Example |
|:---|:---|:---|:---|
| query | The PostGIS SQL SELECT statement the map is based on | No | SELECT d.nom_departement, ... AS pct_pop_femme, d.geom FROM REGION r JOIN ... DEPARTEMENT d ... |
| title | Short, human-readable map title summarizing what the map shows, phrased the way a map legend or panel heading would read (not a restatement of the query) | No | Proportion of women by department |
| style | The MapLibre GL style object (layers, paint, layout) built from the resolved geometry type, value attribute, and display attribute | No | { "layers": [ { "id": "departements", "type": "fill", "paint": { "fill-color": [...] } } ] } |
| recommended_fields | Frontend-facing metadata (not part of the MapLibre spec) indicating which attribute to use as an on-map label and which attribute(s) are worth surfacing in a tooltip/popup | Yes | { "label": "nom_departement", "tooltip": ["nom_departement", "pct_pop_femme"] } |

| Output Field | Type | Description | Optional |
|:---|:---|:---|:---|
| id | string | Unique identifier of the stored (query, title, style, recommended_fields) tuple, to be used by the frontend to retrieve the map data and style | No |

### Sub agent workflow diagram

```mermaid
flowchart TB
    QUESTION(&#91query, question, value_attribute?, display_attribute?&#93) --> AGENT

    subgraph AGENT[Sub Agent]
        direction TB

        PROMPT(Prompt: You are an AI assistant specialized in MapLibre GL style generation for PostGIS query results.)

        T1_INPUT(query,<br>value_attribute?,<br>display_attribute?)
        T1_TOOL[data_profiler]
        T1_OUTPUT(geometry_type,<br>srid,<br>bbox,<br>n_features,<br>columns,<br>value_attribute profile?,<br>display_attribute profile?)

        T2_INPUT(query,<br>title,<br>style,<br>recommended_fields)
        T2_TOOL[store_map]
        T2_OUTPUT(id)

        R(R: stored map id)

        subgraph TOOLS[Tools]
            direction TB
            subgraph T1[Get data profile from query and attributes]
                direction LR
                T1_INPUT --> T1_TOOL
                T1_TOOL --> T1_OUTPUT
            end
            subgraph T2[Store map: query, title, style, complementary metadata]
                direction LR
                T2_INPUT --> T2_TOOL
                T2_TOOL --> T2_OUTPUT
            end
        T1 --> | LLM: design the style and complementary metadata | T2
        T2 --> R
        end
    PROMPT --> TOOLS
    end
```



