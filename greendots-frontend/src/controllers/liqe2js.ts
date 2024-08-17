import type { FieldToken, ImplicitFieldToken, LiqeQuery } from '@/utils/liqe-vendored/types';
import { TEST_ITEM_FILTERABLE_FIELDS } from './TestDataController';

export function liqe_to_function(query: LiqeQuery): (obj: any) => boolean {
  const expr = liqe_to_expr(query);
  return new Function('obj', `return ${expr}`) as (obj: any) => boolean;
}

export function liqe_to_expr(query: LiqeQuery): string {
  switch (query.type) {
    case 'LogicalExpression':
      switch (query.operator.operator) {
        case 'AND':
          return `(${liqe_to_expr(query.left)} && ${liqe_to_expr(query.right)})`;
        case 'OR':
          return `(${liqe_to_expr(query.left)} || ${liqe_to_expr(query.right)})`;
        default:
          throw new Error(`Unsupported operator in: ${JSON.stringify(query)}`);
      }
    case 'UnaryOperator':
      switch (query.operator) {
        case 'NOT':
        case '-':
          return `!(${liqe_to_expr(query.operand)})`;
        default:
          throw new Error(`Unsupported operator in: ${JSON.stringify(query)}`);
      }
    case 'ParenthesizedExpression':
      return `(${liqe_to_expr(query.expression)})`;
    case 'Tag':
      switch (query.operator?.operator) {
        case undefined:
        case ':': {
          const field = field_to_js(query.field);
          switch (query.expression.type) {
            case 'LiteralExpression':
              return `(${field} == ${JSON.stringify(query.expression.value)})`;
            case 'RegexExpression':
              if (query.expression.value.startsWith('//')) {
                return `(!!${field})`;
              }
              return `${query.expression.value}.test(${field})`;
            case 'EmptyExpression':
              return `(!!${field})`;
            default:
              throw new Error(`Unsupported expression type in: ${JSON.stringify(query)}`);
          }
        }
        default:
          throw new Error(`Unsupported operator in: ${JSON.stringify(query)}`);
      }
    default:
      throw new Error(`Unsupported query type in: ${JSON.stringify(query)}`);
  }
}

function field_to_js(field: FieldToken | ImplicitFieldToken): string {
  if (field.type === 'Field') {
    let field_name = field.name;
    if (!TEST_ITEM_FILTERABLE_FIELDS.some((f) => field_name == f)) {
      field_name = 'params.' + field_name;
    }
    return `obj.${field_name}`;
  } else {
    // TODO: replace with "search in all fields"
    return `obj.name`;
  }
}
