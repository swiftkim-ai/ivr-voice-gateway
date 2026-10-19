/**
 * A validator for the subset of JSON Schema the v1 contract uses: a oneOf of
 * closed object definitions, each picked by its `type` constant.
 *
 * It mirrors internal/wire/validate.go deliberately. Both read
 * schema/ivrvg.v1.schema.json, so the two languages agree on one file rather
 * than on two hand-maintained opinions of it. A general-purpose validator
 * would be a dependency for a schema we control and whose shape is fixed.
 */

export interface SchemaNode {
  $ref?: string;
  type?: string;
  const?: unknown;
  enum?: unknown[];
  pattern?: string;
  format?: string;
  minimum?: number;
  minLength?: number;
  maxLength?: number;
  required?: string[];
  properties?: Record<string, SchemaNode>;
  additionalProperties?: boolean;
}

export interface Contract {
  oneOf: { $ref: string }[];
  $defs: Record<string, SchemaNode>;
}

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

export class ContractSchema {
  private readonly byType = new Map<string, SchemaNode>();
  readonly types: string[] = [];
  // Declared explicitly rather than as a constructor parameter property:
  // Node runs these tests by stripping types, which cannot emit the
  // assignment a parameter property implies.
  private readonly contract: Contract;

  constructor(contract: Contract) {
    this.contract = contract;
    for (const branch of contract.oneOf) {
      const def = this.resolve({ $ref: branch.$ref });
      const name = def.properties?.type?.const;
      if (typeof name !== "string") {
        throw new Error(`schema branch ${branch.$ref} has no type constant`);
      }
      this.byType.set(name, def);
      this.types.push(name);
    }
  }

  /** Property names the contract declares for one message type. */
  propertiesOf(type: string): string[] {
    const def = this.byType.get(type);
    if (!def) throw new Error(`${type} is not a message type in v1`);
    return Object.keys(def.properties ?? {});
  }

  /** Returns null when the message is valid, or a description of the fault. */
  validate(value: unknown): string | null {
    if (!isObject(value)) return "not an object";
    const type = value.type;
    if (typeof type !== "string" || type === "") return "message has no type";
    const def = this.byType.get(type);
    if (!def) return `${JSON.stringify(type)} is not a message type in v1`;
    return this.checkObject(def, value, type);
  }

  private resolve(node: SchemaNode): SchemaNode {
    if (!node.$ref) return node;
    const name = node.$ref.replace("#/$defs/", "");
    const target = this.contract.$defs[name];
    if (!target) throw new Error(`$ref ${node.$ref} does not resolve`);
    return target;
  }

  private checkObject(
    def: SchemaNode,
    obj: Record<string, unknown>,
    type: string,
  ): string | null {
    for (const name of def.required ?? []) {
      if (!(name in obj)) return `${type} is missing the required field "${name}"`;
    }
    const closed = def.additionalProperties === false;
    for (const [name, v] of Object.entries(obj)) {
      const prop = def.properties?.[name];
      if (!prop) {
        if (closed) return `${type} carries "${name}", which v1 does not define`;
        continue;
      }
      const fault = this.checkValue(this.resolve(prop), v);
      if (fault) return `${type} field "${name}": ${fault}`;
    }
    return null;
  }

  private checkValue(def: SchemaNode, v: unknown): string | null {
    if (def.const !== undefined && v !== def.const) {
      return `must be ${String(def.const)}, got ${String(v)}`;
    }
    if (def.enum && !def.enum.includes(v)) {
      return `${String(v)} is not one of ${def.enum.join(", ")}`;
    }
    switch (def.type) {
      case "string":
        return this.checkString(def, v);
      case "integer":
        if (typeof v !== "number" || !Number.isInteger(v)) {
          return `must be a whole number, got ${JSON.stringify(v)}`;
        }
        if (def.minimum !== undefined && v < def.minimum) {
          return `must be at least ${def.minimum}, got ${v}`;
        }
        return null;
      case "boolean":
        return typeof v === "boolean" ? null : `must be a boolean, got ${typeof v}`;
      case "object":
        return isObject(v) ? null : `must be an object, got ${typeof v}`;
      case undefined:
        return null;
      default:
        return `unsupported schema type "${def.type}"; teach the validator about it`;
    }
  }

  private checkString(def: SchemaNode, v: unknown): string | null {
    if (typeof v !== "string") return `must be a string, got ${typeof v}`;
    if (def.minLength !== undefined && v.length < def.minLength) {
      return `must be at least ${def.minLength} characters`;
    }
    if (def.maxLength !== undefined && v.length > def.maxLength) {
      return `must be at most ${def.maxLength} characters`;
    }
    if (def.pattern && !new RegExp(def.pattern).test(v)) {
      return `${JSON.stringify(v)} does not match ${def.pattern}`;
    }
    // Timestamps order the event log and drive replay, so a malformed one is
    // worth catching here rather than on the way out.
    if (def.format === "date-time" && Number.isNaN(Date.parse(v))) {
      return `${JSON.stringify(v)} is not an RFC 3339 timestamp`;
    }
    return null;
  }
}
